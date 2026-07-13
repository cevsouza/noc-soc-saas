package api

import (
	"encoding/json"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Incident is the grouped-investigation view (Fase 3/3b): the recurring alerts of the same
// underlying problem (same fingerprint) collapsed into a single record with a worst-severity and
// an alert count. Complements the raw per-event alerts feed.
type Incident struct {
	ID          uuid.UUID  `json:"id"`
	Fingerprint string     `json:"fingerprint"`
	Title       string     `json:"title"`
	Severity    string     `json:"severity"`
	Status      string     `json:"status"`
	AlertCount  int        `json:"alert_count"`
	FirstSeen   time.Time  `json:"first_seen"`
	LastSeen    time.Time  `json:"last_seen"`
	CreatedAt   time.Time  `json:"created_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
}

// HandleGetIncidents lists the tenant's incidents (grouped alerts). Defaults to the most recently
// active first; pass ?status=open|acknowledged|resolved to filter, or ?status=all for everything.
func HandleGetIncidents(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		statusFilter := r.URL.Query().Get("status")
		if statusFilter == "" {
			statusFilter = "all"
		}

		list := make([]Incident, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT id, fingerprint, title, severity, status, alert_count, first_seen, last_seen, created_at, resolved_at
				FROM incidents
				WHERE tenant_id = $1 AND ($2 = 'all' OR status = $2)
				ORDER BY last_seen DESC
				LIMIT 100
			`, tenantID, statusFilter)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var inc Incident
				if err := rows.Scan(&inc.ID, &inc.Fingerprint, &inc.Title, &inc.Severity, &inc.Status,
					&inc.AlertCount, &inc.FirstSeen, &inc.LastSeen, &inc.CreatedAt, &inc.ResolvedAt); err != nil {
					return err
				}
				list = append(list, inc)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to query incidents", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}
