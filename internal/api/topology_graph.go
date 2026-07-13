package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Topology graph (discovery slice C). Merges the three real sources into one node/edge graph:
//   - discovered_devices  (slice A): every SNMP responder found by the active sweep — including gear
//     that never sent an alert.
//   - alert-derived hosts (existing /topology): assets that have reported telemetry (alerts) in the
//     window, carrying current severity.
//   - discovered_links    (slice B): the physical LLDP/CDP edges between devices.
// A device and an alert-host are the same node when the host string matches the device's IP or
// sysName (origin "both"); LLDP/CDP neighbours that aren't themselves discovered become light
// "neighbour" nodes so the edge still has a target.

// GraphNodeIn is a discovered device fed to the pure builder.
type GraphNodeIn struct {
	IP         string
	SysName    string
	Vendor     string
	DeviceType string
}

// GraphLinkIn is a physical edge fed to the pure builder.
type GraphLinkIn struct {
	LocalIP         string
	LocalPort       string
	RemoteSysName   string
	RemoteChassisID string
	RemotePortID    string
	Protocol        string
}

// GraphHostIn is an alert-derived asset fed to the pure builder.
type GraphHostIn struct {
	Host             string
	UnresolvedAlerts int
	WorstSeverity    string
}

// GraphNode is one asset in the merged topology graph.
type GraphNode struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Kind             string `json:"kind"`   // device_type, or "host" / "neighbor"
	Vendor           string `json:"vendor,omitempty"`
	Origin           string `json:"origin"` // "discovery" | "telemetry" | "both" | "neighbor"
	WorstSeverity    string `json:"worst_severity"`
	UnresolvedAlerts int    `json:"unresolved_alerts"`
}

// GraphEdge is one physical adjacency between two nodes.
type GraphEdge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Protocol   string `json:"protocol"`
	LocalPort  string `json:"local_port,omitempty"`
	RemotePort string `json:"remote_port,omitempty"`
}

// TopologyGraph is the merged node/edge graph for a tenant.
type TopologyGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// buildTopologyGraph merges devices, physical links, and alert hosts into one graph. Pure and
// unit-tested (no DB). Node ids are stable: a discovered device's id is its IP; a telemetry-only host's
// id is the host string; a neighbour-only node's id is "nbr:"+identity.
func buildTopologyGraph(devices []GraphNodeIn, links []GraphLinkIn, hosts []GraphHostIn) TopologyGraph {
	nodes := map[string]*GraphNode{}
	// Index for matching: sysName(lower) -> node id, and ip -> node id.
	sysToID := map[string]string{}
	ipToID := map[string]string{}

	for _, d := range devices {
		label := d.SysName
		if label == "" {
			label = d.IP
		}
		n := &GraphNode{ID: d.IP, Label: label, Kind: d.DeviceType, Vendor: d.Vendor, Origin: "discovery"}
		nodes[d.IP] = n
		ipToID[d.IP] = d.IP
		if d.SysName != "" {
			sysToID[strings.ToLower(d.SysName)] = d.IP
		}
	}

	// Fold alert hosts onto matching devices (origin "both"), else add telemetry-only nodes.
	for _, h := range hosts {
		if h.Host == "" {
			continue
		}
		id := ""
		if v, ok := ipToID[h.Host]; ok {
			id = v
		} else if v, ok := sysToID[strings.ToLower(h.Host)]; ok {
			id = v
		}
		if id != "" {
			n := nodes[id]
			n.Origin = "both"
			n.WorstSeverity = h.WorstSeverity
			n.UnresolvedAlerts = h.UnresolvedAlerts
			continue
		}
		nodes[h.Host] = &GraphNode{
			ID: h.Host, Label: h.Host, Kind: "host", Origin: "telemetry",
			WorstSeverity: h.WorstSeverity, UnresolvedAlerts: h.UnresolvedAlerts,
		}
		sysToID[strings.ToLower(h.Host)] = h.Host
	}

	// Physical edges. Resolve the remote endpoint to an existing node by sysName; otherwise create a
	// light neighbour node keyed by chassis id (or sysName) so the edge still lands somewhere.
	edgeSeen := map[string]bool{}
	edges := make([]GraphEdge, 0, len(links))
	for _, l := range links {
		src := l.LocalIP
		if _, ok := nodes[src]; !ok {
			// A link whose local device wasn't in the inventory this cycle: add it as a bare node.
			nodes[src] = &GraphNode{ID: src, Label: src, Kind: "network_device", Origin: "discovery"}
		}
		var tgt string
		if id, ok := sysToID[strings.ToLower(l.RemoteSysName)]; ok && l.RemoteSysName != "" {
			tgt = id
		} else {
			identity := l.RemoteChassisID
			if identity == "" {
				identity = l.RemoteSysName
			}
			tgt = "nbr:" + identity
			if _, ok := nodes[tgt]; !ok {
				label := l.RemoteSysName
				if label == "" {
					label = l.RemoteChassisID
				}
				nodes[tgt] = &GraphNode{ID: tgt, Label: label, Kind: "neighbor", Origin: "neighbor"}
			}
		}
		key := src + "|" + tgt + "|" + l.Protocol
		if edgeSeen[key] || src == tgt {
			continue
		}
		edgeSeen[key] = true
		edges = append(edges, GraphEdge{Source: src, Target: tgt, Protocol: l.Protocol, LocalPort: l.LocalPort, RemotePort: l.RemotePortID})
	}

	out := TopologyGraph{Nodes: make([]GraphNode, 0, len(nodes)), Edges: edges}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, *n)
	}
	// Stable ordering: unresolved first, then by id, so the layout is deterministic.
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].UnresolvedAlerts != out.Nodes[j].UnresolvedAlerts {
			return out.Nodes[i].UnresolvedAlerts > out.Nodes[j].UnresolvedAlerts
		}
		return out.Nodes[i].ID < out.Nodes[j].ID
	})
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].Source != out.Edges[j].Source {
			return out.Edges[i].Source < out.Edges[j].Source
		}
		return out.Edges[i].Target < out.Edges[j].Target
	})
	return out
}

// HandleGetTopologyGraph returns the merged topology graph (discovery slice C). Same access level and
// tenant-scoping as the other topology/SLA endpoints.
func HandleGetTopologyGraph(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var devices []GraphNodeIn
		var links []GraphLinkIn
		var hosts []GraphHostIn

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			dr, e := tx.Query(ctx, `SELECT ip, sysname, vendor, device_type FROM discovered_devices WHERE tenant_id = $1 LIMIT 500`, tenantID)
			if e != nil {
				return e
			}
			for dr.Next() {
				var d GraphNodeIn
				if e := dr.Scan(&d.IP, &d.SysName, &d.Vendor, &d.DeviceType); e != nil {
					dr.Close()
					return e
				}
				devices = append(devices, d)
			}
			dr.Close()

			lr, e := tx.Query(ctx, `SELECT local_ip, local_port, remote_sysname, remote_chassis_id, remote_port_id, protocol FROM discovered_links WHERE tenant_id = $1 LIMIT 1000`, tenantID)
			if e != nil {
				return e
			}
			for lr.Next() {
				var l GraphLinkIn
				if e := lr.Scan(&l.LocalIP, &l.LocalPort, &l.RemoteSysName, &l.RemoteChassisID, &l.RemotePortID, &l.Protocol); e != nil {
					lr.Close()
					return e
				}
				links = append(links, l)
			}
			lr.Close()

			// Alert-derived hosts (same window/exclusions as HandleGetTopology).
			window := fmt.Sprintf("%d days", topologyWindowDays)
			hr, e := tx.Query(ctx, `
				SELECT
					NULLIF(ai_analysis->>'host', '') AS host,
					COUNT(*) FILTER (WHERE status <> 'resolved') AS unresolved,
					MAX(CASE WHEN status <> 'resolved' THEN
						CASE severity WHEN 'fatal' THEN 4 WHEN 'critical' THEN 3 WHEN 'warning' THEN 2 WHEN 'info' THEN 1 ELSE 0 END
						ELSE 0 END) AS worst_rank
				FROM alerts
				WHERE tenant_id = $1
				  AND created_at >= NOW() - $2::interval
				  AND COALESCE(ai_analysis->>'host', '') <> ''
				  AND COALESCE(ai_analysis->>'source', '') <> 'system'
				GROUP BY host
				LIMIT 200
			`, tenantID, window)
			if e != nil {
				return e
			}
			for hr.Next() {
				var h GraphHostIn
				var rank int
				if e := hr.Scan(&h.Host, &h.UnresolvedAlerts, &rank); e != nil {
					hr.Close()
					return e
				}
				h.WorstSeverity = severityRankToString(rank)
				hosts = append(hosts, h)
			}
			hr.Close()
			return nil
		})
		if err != nil {
			http.Error(w, "Failed to build topology graph", http.StatusInternalServerError)
			return
		}

		graph := buildTopologyGraph(devices, links, hosts)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(graph)
	}
}
