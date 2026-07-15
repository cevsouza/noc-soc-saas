package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/domain"
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
	// AssetCriticality is the business criticality of the managed CMDB asset this incident hits (T2),
	// when its host resolves to one — it's the extra input (beyond severity + recurrence) that raised
	// the risk score (B1). Empty when the host isn't a managed asset.
	AssetCriticality string     `json:"asset_criticality,omitempty"`
	AlertCount       int        `json:"alert_count"`
	FirstSeen        time.Time  `json:"first_seen"`
	LastSeen         time.Time  `json:"last_seen"`
	CreatedAt        time.Time  `json:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
	// Disposition is the analyst's verdict (K5): "true_positive", "false_positive", or "benign".
	// Empty when not yet classified. Feeds the false-positive-rate KPI.
	Disposition string `json:"disposition,omitempty"`
}

// validDispositions is the closed set of analyst verdicts a K5 classification may take.
var validDispositions = map[string]bool{"true_positive": true, "false_positive": true, "benign": true}

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
		// Segregated NOC/SOC console: ?domain=noc|soc keeps only incidents whose grouped alerts come
		// from that domain's sources (an incident groups one fingerprint = one source, so its domain
		// is well-defined). Filtered via an EXISTS on the incident's alerts.
		domainClause := ""
		args := []interface{}{tenantID, statusFilter}
		if sources, ok := domain.SourcesForDomain(r.URL.Query().Get("domain")); ok {
			args = append(args, sources)
			domainClause = ` AND EXISTS (SELECT 1 FROM alerts a WHERE a.tenant_id = incidents.tenant_id AND a.incident_id = incidents.id AND a.ai_analysis->>'source' = ANY($3))`
		}

		list := make([]Incident, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT id, fingerprint, title, severity, status, risk_score, COALESCE(asset_criticality, ''), alert_count, first_seen, last_seen, created_at, resolved_at, COALESCE(disposition, '')
				FROM incidents
				WHERE tenant_id = $1 AND ($2 = 'all' OR status = $2)`+domainClause+`
				ORDER BY risk_score DESC, last_seen DESC
				LIMIT 100
			`, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var inc Incident
				if err := rows.Scan(&inc.ID, &inc.Fingerprint, &inc.Title, &inc.Severity, &inc.Status, &inc.RiskScore, &inc.AssetCriticality,
					&inc.AlertCount, &inc.FirstSeen, &inc.LastSeen, &inc.CreatedAt, &inc.ResolvedAt, &inc.Disposition); err != nil {
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

// IncidentDispositionRequest is the body of the classify endpoint.
type IncidentDispositionRequest struct {
	IncidentID  uuid.UUID `json:"incident_id"`
	Disposition string    `json:"disposition"`
}

// HandleSetIncidentDisposition records the analyst's verdict (K5) on an incident — true_positive,
// false_positive, or benign — used by the false-positive-rate KPI. Works on an incident in any state
// (classify at resolution time or later, and reclassify if needed). Audited; posts a timeline note.
func HandleSetIncidentDisposition(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req IncidentDispositionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.IncidentID == uuid.Nil || !validDispositions[req.Disposition] {
			http.Error(w, "Invalid payload: incident_id and a valid disposition (true_positive|false_positive|benign) are required", http.StatusBadRequest)
			return
		}

		var affected int64
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			res, e := tx.Exec(ctx, `
				UPDATE incidents SET disposition = $1, updated_at = NOW()
				WHERE id = $2 AND tenant_id = $3
			`, req.Disposition, req.IncidentID, tenantID)
			if e != nil {
				return e
			}
			affected = res.RowsAffected()
			if affected > 0 {
				_, e = tx.Exec(ctx, `
					INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
					VALUES ($1, $2, 'Sistema', $3)
				`, req.IncidentID, tenantID, "Incidente classificado como: "+req.Disposition)
			}
			return e
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to classify incident: %v", err), http.StatusInternalServerError)
			return
		}
		if affected == 0 {
			http.Error(w, "Incident not found or not yours", http.StatusNotFound)
			return
		}

		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}
		audit.Record(ctx, pgPool, audit.Entry{
			TenantID: tenantID, UserID: actorID,
			Action:    "incident.classify",
			Resource:  req.IncidentID.String(),
			Details:   map[string]interface{}{"disposition": req.Disposition},
			IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "disposition": req.Disposition})
	}
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
