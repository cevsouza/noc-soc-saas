package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// topologyStaleThreshold: a discovered device the agent hasn't re-observed within this window is
// "stale" (likely decommissioned, or the agent stopped scanning it) — flagged so a dead box doesn't
// masquerade as an operational node. The discovery sweep runs ~15min, so 24h tolerates transient
// agent hiccups without false positives.
const topologyStaleThreshold = 24 * time.Hour

// Topology graph (discovery slice C; robust matching in slice T3). Merges the three real sources into
// one node/edge graph:
//   - discovered_devices  (slice A): every SNMP responder found by the active sweep.
//   - alert-derived hosts (existing /topology): assets that have reported telemetry in the window.
//   - discovered_links    (slice B): the physical LLDP/CDP edges between devices.
// A device and an alert-host collapse to one node when the host resolves to the device — by exact IP,
// by sysName, by short-name (FQDN vs bare hostname), or by a CMDB alias the operator set (slice T3).
// Without this, a box shows up twice: once discovered, once as a telemetry-only host.

// GraphNodeIn is a discovered device fed to the pure builder.
type GraphNodeIn struct {
	IP         string
	SysName    string
	Vendor     string
	DeviceType string
	LastSeen   time.Time
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

// AssetAlias carries the manual host↔device mapping from the CMDB (slice T3): an asset identifier and
// the alternate hostnames a monitoring tool uses for it.
type AssetAlias struct {
	Identifier string
	Aliases    []string
}

// GraphNode is one asset in the merged topology graph.
type GraphNode struct {
	ID               string     `json:"id"`
	Label            string     `json:"label"`
	Kind             string     `json:"kind"`   // device_type, or "host" / "neighbor"
	Vendor           string     `json:"vendor,omitempty"`
	Origin           string     `json:"origin"` // "discovery" | "telemetry" | "both" | "neighbor"
	WorstSeverity    string     `json:"worst_severity"`
	UnresolvedAlerts int        `json:"unresolved_alerts"`
	// Stale marks a discovered device the agent hasn't re-observed within topologyStaleThreshold
	// (T-B). LastSeen is the last discovery timestamp (nil for telemetry/neighbour nodes).
	Stale    bool       `json:"stale"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
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

// normalizeHost lowercases, trims, and drops a trailing dot (FQDN root) so "Edge-FW.corp." and
// "edge-fw.corp" compare equal.
func normalizeHost(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// shortName returns the first DNS label ("edge-fw.corp.example.com" -> "edge-fw"). Assumes a normalized
// input. Callers must not apply this to IP addresses.
func shortName(s string) string {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i]
	}
	return s
}

// severityStringRank ranks a severity label for aggregation (higher = worse). "" ranks 0.
func severityStringRank(s string) int {
	switch s {
	case "fatal":
		return 4
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// hostResolver folds an alert-host / neighbour name onto an existing device (or telemetry) node id.
type hostResolver struct {
	ipToID    map[string]string // exact IP -> node id
	sysToID   map[string]string // normalized sysName/host -> node id
	shortToID map[string]string // unambiguous short-name -> device node id
	aliasToID map[string]string // normalized CMDB alias (and its short form) -> device node id
}

// resolve returns the node id a name maps to, if any. Order: exact IP, exact (normalized) name, alias,
// short-name. Short-name matching is skipped for IP literals so "192.168.1.1" never collapses on "192".
func (hr hostResolver) resolve(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if id, ok := hr.ipToID[name]; ok {
		return id, true
	}
	n := normalizeHost(name)
	if id, ok := hr.sysToID[n]; ok {
		return id, true
	}
	if id, ok := hr.aliasToID[n]; ok {
		return id, true
	}
	if net.ParseIP(name) != nil {
		return "", false
	}
	s := shortName(n)
	if id, ok := hr.aliasToID[s]; ok {
		return id, true
	}
	if id, ok := hr.shortToID[s]; ok {
		return id, true
	}
	return "", false
}

// buildTopologyGraph merges devices, physical links, alert hosts, and CMDB aliases into one graph. A
// discovered device whose LastSeen is before staleBefore is flagged Stale (T-B). Pure and unit-tested
// (no DB) — the caller passes staleBefore = now - threshold so time is injectable.
func buildTopologyGraph(devices []GraphNodeIn, links []GraphLinkIn, hosts []GraphHostIn, aliases []AssetAlias, staleBefore time.Time) TopologyGraph {
	nodes := map[string]*GraphNode{}
	hr := hostResolver{
		ipToID:    map[string]string{},
		sysToID:   map[string]string{},
		shortToID: map[string]string{},
		aliasToID: map[string]string{},
	}
	shortCount := map[string]int{} // detect ambiguous short-names (2+ devices) and drop them

	for _, d := range devices {
		label := d.SysName
		if label == "" {
			label = d.IP
		}
		n := &GraphNode{ID: d.IP, Label: label, Kind: d.DeviceType, Vendor: d.Vendor, Origin: "discovery"}
		if !d.LastSeen.IsZero() {
			ls := d.LastSeen
			n.LastSeen = &ls
			n.Stale = !staleBefore.IsZero() && d.LastSeen.Before(staleBefore)
		}
		nodes[d.IP] = n
		hr.ipToID[d.IP] = d.IP
		if d.SysName != "" {
			ns := normalizeHost(d.SysName)
			hr.sysToID[ns] = d.IP
			sn := shortName(ns)
			shortCount[sn]++
			hr.shortToID[sn] = d.IP
		}
	}
	for sn, c := range shortCount {
		if c > 1 {
			delete(hr.shortToID, sn) // ambiguous: never guess between two devices
		}
	}

	// CMDB aliases: map each alias (and its short form) to the device the asset identifier points at.
	for _, a := range aliases {
		target, ok := hr.ipToID[a.Identifier]
		if !ok {
			target, ok = hr.sysToID[normalizeHost(a.Identifier)]
		}
		if !ok {
			continue // alias for a non-discovered (manual) asset — not a topology node
		}
		for _, al := range a.Aliases {
			na := normalizeHost(al)
			if na == "" {
				continue
			}
			hr.aliasToID[na] = target
			if net.ParseIP(al) == nil {
				hr.aliasToID[shortName(na)] = target
			}
		}
	}

	// Fold alert hosts onto matching devices (origin "both"), else add telemetry-only nodes.
	for _, h := range hosts {
		if h.Host == "" {
			continue
		}
		if id, ok := hr.resolve(h.Host); ok {
			// Multiple alert-hosts can map to the same device (FQDN + alias + short-name); aggregate
			// instead of overwriting so the node reflects the total open alerts and the worst severity.
			n := nodes[id]
			n.Origin = "both"
			n.UnresolvedAlerts += h.UnresolvedAlerts
			if severityStringRank(h.WorstSeverity) > severityStringRank(n.WorstSeverity) {
				n.WorstSeverity = h.WorstSeverity
			}
			continue
		}
		nodes[h.Host] = &GraphNode{
			ID: h.Host, Label: h.Host, Kind: "host", Origin: "telemetry",
			WorstSeverity: h.WorstSeverity, UnresolvedAlerts: h.UnresolvedAlerts,
		}
		hr.sysToID[normalizeHost(h.Host)] = h.Host
	}

	// Physical edges. Resolve the remote endpoint to an existing node; otherwise create a light
	// neighbour node keyed by chassis id (or sysName) so the edge still lands somewhere.
	edgeSeen := map[string]bool{}
	edges := make([]GraphEdge, 0, len(links))
	for _, l := range links {
		src := l.LocalIP
		if _, ok := nodes[src]; !ok {
			nodes[src] = &GraphNode{ID: src, Label: src, Kind: "network_device", Origin: "discovery"}
			hr.ipToID[src] = src
		}
		var tgt string
		if id, ok := hr.resolve(l.RemoteSysName); ok {
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

// HandleGetTopologyGraph returns the merged topology graph. Same access level and tenant-scoping as the
// other topology/SLA endpoints.
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
		var aliases []AssetAlias

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			dr, e := tx.Query(ctx, `SELECT ip, sysname, vendor, device_type, last_seen FROM discovered_devices WHERE tenant_id = $1 LIMIT 500`, tenantID)
			if e != nil {
				return e
			}
			for dr.Next() {
				var d GraphNodeIn
				if e := dr.Scan(&d.IP, &d.SysName, &d.Vendor, &d.DeviceType, &d.LastSeen); e != nil {
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

			// CMDB aliases for manual host↔device mapping (slice T3).
			aar, e := tx.Query(ctx, `SELECT identifier, aliases FROM assets WHERE tenant_id = $1 AND array_length(aliases, 1) > 0`, tenantID)
			if e != nil {
				return e
			}
			for aar.Next() {
				var a AssetAlias
				if e := aar.Scan(&a.Identifier, &a.Aliases); e != nil {
					aar.Close()
					return e
				}
				aliases = append(aliases, a)
			}
			aar.Close()

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

		graph := buildTopologyGraph(devices, links, hosts, aliases, time.Now().Add(-topologyStaleThreshold))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(graph)
	}
}
