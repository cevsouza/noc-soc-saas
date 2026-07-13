package api

import (
	"encoding/json"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Topology window matches the SLA/operational reports (30 days) so all three describe the same
// period. Shared by the merged topology graph (topology_graph.go) for the alert-derived hosts.
const topologyWindowDays = 30

// severityRankToString maps the numeric rank used in the SQL aggregation back to a severity label.
// Kept pure and separate so it is unit-testable without a database. 0 means "no unresolved alert".
func severityRankToString(rank int) string {
	switch rank {
	case 4:
		return "fatal"
	case 3:
		return "critical"
	case 2:
		return "warning"
	case 1:
		return "info"
	default:
		return ""
	}
}

// agentFreshSeconds is how recently an agent must have checked in to count as "connected". Agents
// poll config / heartbeat on a short interval (default 60s), so a few minutes of grace covers a
// missed cycle without flapping.
const agentFreshSeconds = 300

// TopologyStatus tells the topology tab whether active discovery is actually wired up: is an agent
// connected, when did it last check in, and how much has it found. This drives the onboarding state
// so an empty graph explains itself (no agent / no ranges) instead of degrading silently.
type TopologyStatus struct {
	AgentCount        int        `json:"agent_count"`
	AgentConnected    bool       `json:"agent_connected"`
	LastSeen          *time.Time `json:"last_seen"`
	DiscoveryTargets  int        `json:"discovery_targets"`
	DiscoveredDevices int        `json:"discovered_devices"`
	DiscoveredLinks   int        `json:"discovered_links"`
}

// HandleGetTopologyStatus returns the discovery wiring status for the tenant. Same authenticated
// access level and tenant-scoping as the topology graph endpoint.
func HandleGetTopologyStatus(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var status TopologyStatus
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var lastSeen *time.Time
			if e := tx.QueryRow(ctx,
				`SELECT COUNT(*), MAX(last_seen) FROM agents WHERE tenant_id = $1`, tenantID,
			).Scan(&status.AgentCount, &lastSeen); e != nil {
				return e
			}
			status.LastSeen = lastSeen
			if lastSeen != nil && time.Since(*lastSeen) <= agentFreshSeconds*time.Second {
				status.AgentConnected = true
			}
			if e := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM agent_discovery_targets WHERE tenant_id = $1`, tenantID,
			).Scan(&status.DiscoveryTargets); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM discovered_devices WHERE tenant_id = $1`, tenantID,
			).Scan(&status.DiscoveredDevices); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx,
				`SELECT COUNT(*) FROM discovered_links WHERE tenant_id = $1`, tenantID,
			).Scan(&status.DiscoveredLinks); e != nil {
				return e
			}
			return nil
		})
		if err != nil {
			http.Error(w, "Failed to compute topology status", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}
}
