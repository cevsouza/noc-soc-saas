package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"noc-api/internal/audit"
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
	RiskScore   int        `json:"risk_score"`
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
				SELECT id, fingerprint, title, severity, status, risk_score, alert_count, first_seen, last_seen, created_at, resolved_at
				FROM incidents
				WHERE tenant_id = $1 AND ($2 = 'all' OR status = $2)
				ORDER BY risk_score DESC, last_seen DESC
				LIMIT 100
			`, tenantID, statusFilter)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var inc Incident
				if err := rows.Scan(&inc.ID, &inc.Fingerprint, &inc.Title, &inc.Severity, &inc.Status, &inc.RiskScore,
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

// IncidentActionRequest is the body of the acknowledge/resolve endpoints.
type IncidentActionRequest struct {
	IncidentID uuid.UUID `json:"incident_id"`
}

// updateIncidentStatus is shared by acknowledge and resolve: it flips the incident's status and
// cascades the same transition to the alerts grouped under it (so the alert feed stays consistent),
// records an audit entry, and posts a timeline comment. Resolving closes the incident, which — via
// the partial unique index — lets a fresh recurrence of the same fingerprint open a NEW incident.
func updateIncidentStatus(pgPool *pgxpool.Pool, action, newStatus string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req IncidentActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IncidentID == uuid.Nil {
			http.Error(w, "Invalid payload: incident_id is required", http.StatusBadRequest)
			return
		}

		tsColumn := "acknowledged_at"
		if newStatus == "resolved" {
			tsColumn = "resolved_at"
		}

		var affected int64
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			res, e := tx.Exec(ctx, fmt.Sprintf(`
				UPDATE incidents SET status = $1, %s = NOW(), updated_at = NOW()
				WHERE id = $2 AND tenant_id = $3 AND status <> 'resolved'
			`, tsColumn), newStatus, req.IncidentID, tenantID)
			if e != nil {
				return e
			}
			affected = res.RowsAffected()
			if affected == 0 {
				return nil // not found / already resolved — handled below
			}
			// Cascade the same transition to the incident's alerts.
			if _, e := tx.Exec(ctx, fmt.Sprintf(`
				UPDATE alerts SET status = $1, %s = NOW(), updated_at = NOW()
				WHERE tenant_id = $2 AND incident_id = $3 AND status <> $1
			`, tsColumn), newStatus, tenantID, req.IncidentID); e != nil {
				return e
			}
			return nil
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to %s incident: %v", action, err), http.StatusInternalServerError)
			return
		}
		if affected == 0 {
			http.Error(w, "Incident not found, not yours, or already resolved", http.StatusNotFound)
			return
		}

		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}
		audit.Record(ctx, pgPool, audit.Entry{
			TenantID: tenantID, UserID: actorID,
			Action:    "incident." + action,
			Resource:  req.IncidentID.String(),
			IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "incident_status": newStatus})
	}
}

// HandleAcknowledgeIncident acknowledges an incident and its alerts.
func HandleAcknowledgeIncidentGroup(pgPool *pgxpool.Pool) http.HandlerFunc {
	return updateIncidentStatus(pgPool, "acknowledge", "acknowledged")
}

// HandleResolveIncidentGroup resolves (closes) an incident and its alerts.
func HandleResolveIncidentGroup(pgPool *pgxpool.Pool) http.HandlerFunc {
	return updateIncidentStatus(pgPool, "resolve", "resolved")
}

// IncidentAlert is a slim view of one alert grouped under an incident.
type IncidentAlert struct {
	ID        uuid.UUID `json:"id"`
	EventType string    `json:"event_type"`
	Severity  string    `json:"severity"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// HandleGetIncidentAlerts returns the alerts grouped under an incident (?incident_id=).
func HandleGetIncidentAlerts(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		incidentID, perr := uuid.Parse(r.URL.Query().Get("incident_id"))
		if perr != nil {
			http.Error(w, "Bad Request: valid incident_id is required", http.StatusBadRequest)
			return
		}

		list := make([]IncidentAlert, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `
				SELECT id, event_type, severity, status, summary, created_at
				FROM alerts
				WHERE tenant_id = $1 AND incident_id = $2
				ORDER BY created_at DESC
				LIMIT 200
			`, tenantID, incidentID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var a IncidentAlert
				if e := rows.Scan(&a.ID, &a.EventType, &a.Severity, &a.Status, &a.Summary, &a.CreatedAt); e != nil {
					return e
				}
				list = append(list, a)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to query incident alerts", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}
