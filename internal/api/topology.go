package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Topology window matches the SLA/operational reports (30 days) so all three describe the same
// period. Assets are derived from the tenant's real alert stream (grouped by the host recorded in
// ai_analysis), NOT from a hardcoded diagram — the old topology tab was a static 6-node SVG that
// looked identical for every tenant. This replaces that with the hosts that have actually reported
// telemetry. System-generated alerts (source=system, e.g. the connection watchdog) are excluded so
// the map shows telemetry assets, not internal signals.
const topologyWindowDays = 30

// TopologyNode is one asset (host) that has reported at least one alert in the window.
type TopologyNode struct {
	Host             string    `json:"host"`
	TotalAlerts      int       `json:"total_alerts"`
	UnresolvedAlerts int       `json:"unresolved_alerts"`
	WorstSeverity    string    `json:"worst_severity"` // "" when nothing is currently unresolved
	Sources          []string  `json:"sources"`
	LastSeen         time.Time `json:"last_seen"`
}

// TopologyResponse is the asset map for a tenant.
type TopologyResponse struct {
	WindowDays  int            `json:"window_days"`
	TotalAssets int            `json:"total_assets"`
	Nodes       []TopologyNode `json:"nodes"`
}

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

// HandleGetTopology returns the tenant's real asset map, derived from the hosts present in its
// alert stream over the window. Same access level and tenant-scoping as the SLA/operational stats
// endpoints.
func HandleGetTopology(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		resp := TopologyResponse{WindowDays: topologyWindowDays, Nodes: []TopologyNode{}}

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			window := fmt.Sprintf("%d days", topologyWindowDays)
			rows, err := tx.Query(ctx, `
				SELECT
					NULLIF(ai_analysis->>'host', '')                        AS host,
					COUNT(*)                                               AS total,
					COUNT(*) FILTER (WHERE status <> 'resolved')           AS unresolved,
					MAX(CASE WHEN status <> 'resolved' THEN
						CASE severity
							WHEN 'fatal' THEN 4 WHEN 'critical' THEN 3
							WHEN 'warning' THEN 2 WHEN 'info' THEN 1 ELSE 0 END
						ELSE 0 END)                                         AS worst_rank,
					COALESCE(
						array_remove(array_agg(DISTINCT NULLIF(ai_analysis->>'source','')), NULL),
						'{}')                                              AS sources,
					MAX(created_at)                                        AS last_seen
				FROM alerts
				WHERE tenant_id = $1
				  AND created_at >= NOW() - $2::interval
				  AND COALESCE(ai_analysis->>'host', '') <> ''
				  AND COALESCE(ai_analysis->>'source', '') <> 'system'
				GROUP BY host
				ORDER BY unresolved DESC, total DESC
				LIMIT 40
			`, tenantID, window)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var n TopologyNode
				var worstRank int
				if err := rows.Scan(&n.Host, &n.TotalAlerts, &n.UnresolvedAlerts, &worstRank, &n.Sources, &n.LastSeen); err != nil {
					return err
				}
				n.WorstSeverity = severityRankToString(worstRank)
				if n.Sources == nil {
					n.Sources = []string{}
				}
				resp.Nodes = append(resp.Nodes, n)
			}
			return rows.Err()
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to compute topology: %v", err), http.StatusInternalServerError)
			return
		}

		resp.TotalAssets = len(resp.Nodes)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
