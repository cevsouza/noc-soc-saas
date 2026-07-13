package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/cache"
	"noc-api/internal/connector"
	"noc-api/internal/db"
	"noc-api/internal/domain"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"noc-api/internal/queue"
	"noc-api/internal/repository"
	"noc-api/internal/security"
	"noc-api/internal/ws"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type IngestEventRequest struct {
	DeviceID  *uuid.UUID             `json:"device_id,omitempty"`
	EventType string                 `json:"event_type"`
	Severity  model.AlertSeverity    `json:"severity"`
	Summary   string                 `json:"summary"`
	Payload   map[string]interface{} `json:"payload"`
}

type IngestResponse struct {
	Status    string    `json:"status"`
	ID        uuid.UUID `json:"id"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type IngestBatchResponse struct {
	Status    string      `json:"status"`
	IDs       []uuid.UUID `json:"ids"`
	Message   string      `json:"message"`
	Timestamp time.Time   `json:"timestamp"`
}

// HandleGenericWebhook handles POST /api/v1/webhook/{integration_type}/{tenant_id}
// SECURITY: a valid X-Signature HMAC (keyed with the tenant's own webhook_hmac_secret,
// provisioned via POST /api/v1/integrations/webhook-secret) is always required. Without
// this, any request naming a tenant's public UUID (exposed via /api/v1/public/tenants)
// could inject arbitrary alerts into that tenant with no authentication at all.
func HandleGenericWebhook(pgPool *pgxpool.Pool, redisClient *redis.Client, vaultRepo repository.VaultRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract integration_type and tenant_id from path /api/v1/webhook/{type}/{tenant}
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) < 6 {
			http.Error(w, "Bad Request: Invalid webhook path format", http.StatusBadRequest)
			return
		}
		integrationType := pathParts[4]
		tenantIDStr := pathParts[5]

		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "Bad Request: Invalid tenant UUID", http.StatusBadRequest)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: Failed to read body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		xSignature := r.Header.Get("X-Signature")
		if xSignature == "" {
			http.Error(w, "Unauthorized: X-Signature header is required", http.StatusUnauthorized)
			return
		}

		webhookSecret, err := getTenantWebhookSecret(r.Context(), pgPool, vaultRepo, tenantID)
		if err != nil || webhookSecret == "" {
			http.Error(w, "Forbidden: webhook signing secret not configured for this tenant", http.StatusForbidden)
			return
		}

		mac := hmac.New(sha256.New, []byte(webhookSecret))
		mac.Write(bodyBytes)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(xSignature)) {
			errMsg := "Unauthorized: HMAC signature verification failed"
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error",integrationType), errMsg, 24*time.Hour)
			http.Error(w, errMsg, http.StatusUnauthorized)
			return
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid JSON body: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error",integrationType), errMsg, 24*time.Hour)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}

		rawMsg := queue.RawWebhookMessage{
			TenantID:        tenantID,
			IntegrationType: integrationType,
			Payload:         payload,
		}

		rawMsgBytes, err := json.Marshal(rawMsg)
		if err != nil {
			http.Error(w, "Internal Server Error: marshal failed", http.StatusInternalServerError)
			return
		}

		// Register heartbeat in Redis and clear any previous errors
		ctx := r.Context()
		redisClient.Set(ctx, cache.TenantKey(tenantID, "heartbeat",integrationType), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(ctx, cache.TenantKey(tenantID, "webhook_error",integrationType))

		// Push to alerts.raw queue
		err = redisClient.LPush(ctx, queue.AlertsRawQueueKey, rawMsgBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: failed to queue raw alert", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"Accepted","message":"Raw event dispatched to alerts.raw queue"}`))
	}
}

// HandleIngest handles generic alert ingestion
func HandleIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		// Verify active integration setting
		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "generic").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Generic integration not active for this tenant", http.StatusForbidden)
			return
		}

		var req IngestEventRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: Invalid JSON payload", http.StatusBadRequest)
			return
		}

		if req.EventType == "" || req.Summary == "" {
			http.Error(w, "Bad Request: event_type and summary are required", http.StatusBadRequest)
			return
		}

		switch req.Severity {
		case model.SeverityInfo, model.SeverityWarning, model.SeverityCritical, model.SeverityFatal:
			// Valid
		default:
			http.Error(w, "Bad Request: invalid severity", http.StatusBadRequest)
			return
		}

		if req.Payload == nil {
			req.Payload = make(map[string]interface{})
		}

		eventID := uuid.New()
		eventTime := time.Now()
		incident := model.UnifiedIncident{
			ID:          eventID,
			TenantID:    tenantID,
			DeviceID:    req.DeviceID,
			Source:      "generic",
			ExternalID:  eventID.String(),
			EventType:   req.EventType,
			Severity:    req.Severity,
			Title:       req.Summary,
			Description: req.Summary,
			RawPayload:  req.Payload,
			Timestamp:   eventTime,
		}

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue event", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        eventID,
			Message:   "Generic incident successfully normalized and queued",
			Timestamp: eventTime,
		})
	}
}

// HandlePrometheusIngest normalizes Prometheus/Alertmanager webhook alerts
func HandlePrometheusIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		// Verify active integration setting
		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "prometheus").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Prometheus integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourcePrometheus).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Alertmanager JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","prometheus"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Alertmanager JSON payload", http.StatusBadRequest)
			return
		}

		var incidentIDs []uuid.UUID
		var eventBytesList [][]byte

		for _, incident := range incidents {
			incidentIDs = append(incidentIDs, incident.ID)

			bytes, err := json.Marshal(incident)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			eventBytesList = append(eventBytesList, bytes)
		}

		// Push all to Redis in a single pipeline
		if len(eventBytesList) > 0 {
			pipe := redisClient.Pipeline()
			for _, bytes := range eventBytesList {
				pipe.LPush(r.Context(), queue.AlertsNormalizedQueueKey, bytes)
			}
			_, err := pipe.Exec(r.Context())
			if err != nil {
				http.Error(w, "Internal Server Error: Failed to queue alerts", http.StatusInternalServerError)
				return
			}
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","prometheus"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","prometheus"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestBatchResponse{
			Status:    "accepted",
			IDs:       incidentIDs,
			Message:   fmt.Sprintf("Successfully normalized and queued %d Prometheus alerts", len(incidentIDs)),
			Timestamp: time.Now(),
		})
	}
}

// HandleWazuhIngest normalizes Wazuh security alerts
func HandleWazuhIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		// Verify active integration setting
		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "wazuh").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Wazuh integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceWazuh).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Wazuh JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","wazuh"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Wazuh JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue alert", http.StatusInternalServerError)
			return
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","wazuh"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","wazuh"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "Wazuh security alert successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// HandleDownloadSLAReport triggers PDF generation via Python worker and serves the file
func HandleDownloadSLAReport(pgPool *pgxpool.Pool, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tenantID uuid.UUID
		if tID, ok := db.TenantIDFromContext(r.Context()); ok {
			tenantID = tID
		} else {
			token := r.URL.Query().Get("token")
			resolvedID, err := middleware.ResolveTenantFromToken(token, jwtSecret, pgPool)
			if err != nil {
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}
			tenantID = resolvedID
		}

		tenantName := r.URL.Query().Get("tenant_name")
		if tenantName == "" {
			tenantName = "Enterprise Customer"
		}

		outputFile := fmt.Sprintf("./frontend/public/reports/sla_report_%s.pdf", tenantID.String())

		// Spawn Python SLA report generator
		cmd := exec.CommandContext(r.Context(), "python", "./workers/sla_report_generator.py",
			"--tenant", tenantID.String(),
			"--name", tenantName,
			"--output", outputFile)

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			log.Printf("SLA Generator Subprocess Error: %v. Stderr: %s", err, stderr.String())
			http.Error(w, "Failed to generate report", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=sla_report_%s.pdf", tenantID.String()))
		http.ServeFile(w, r, outputFile)
	}
}

// HandleSLADebug runs diagnostic commands on python/pip environment
func HandleSLADebug(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var out bytes.Buffer
		var stderr bytes.Buffer

		cmd := exec.Command("python", "--version")
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()

		result := map[string]interface{}{
			"python_version_err":     fmt.Sprintf("%v", err),
			"python_version_out":     out.String(),
			"python_version_err_out": stderr.String(),
		}

		out.Reset()
		stderr.Reset()

		cmd2 := exec.Command("pip", "list")
		cmd2.Stdout = &out
		cmd2.Stderr = &stderr
		err2 := cmd2.Run()

		result["pip_list_err"] = fmt.Sprintf("%v", err2)
		result["pip_list_out"] = out.String()
		result["pip_list_err_out"] = stderr.String()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

type IncidentAcknowledgeRequest struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type IncidentResolveRequest struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// HandleAcknowledgeIncident marks an alert/incident as acknowledged and logs the operator action
func HandleAcknowledgeIncident(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req IncidentAcknowledgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		var rowsAffected int64
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := `
				UPDATE alerts
				SET status = 'acknowledged', acknowledged_at = NOW(), updated_at = NOW()
				WHERE id = $1 AND created_at = $2 AND tenant_id = $3
			`
			res, err := tx.Exec(ctx, query, req.ID, req.CreatedAt, tenantID)
			if err != nil {
				return err
			}
			rowsAffected = res.RowsAffected()
			if rowsAffected > 0 {
				logQuery := `
					INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
					VALUES ($1, $2, 'Sistema', 'Incidente RECONHECIDO pelo operador no Cockpit.')
				`
				_, err = tx.Exec(ctx, logQuery, req.ID, tenantID)
				return err
			}
			return nil
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to acknowledge incident: %v", err), http.StatusInternalServerError)
			return
		}

		if rowsAffected == 0 {
			http.Error(w, "Incident not found or doesn't belong to tenant", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Incidente reconhecido com sucesso"})
	}
}

// HandleResolveIncident marks an alert/incident as resolved and logs resolution time
func HandleResolveIncident(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req IncidentResolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		var rowsAffected int64
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := `
				UPDATE alerts
				SET status = 'resolved', resolved_at = NOW(), updated_at = NOW()
				WHERE id = $1 AND created_at = $2 AND tenant_id = $3
			`
			res, err := tx.Exec(ctx, query, req.ID, req.CreatedAt, tenantID)
			if err != nil {
				return err
			}
			rowsAffected = res.RowsAffected()
			if rowsAffected > 0 {
				logQuery := `
					INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
					VALUES ($1, $2, 'Sistema', 'Incidente RESOLVIDO e finalizado pelo operador.')
				`
				if _, err = tx.Exec(ctx, logQuery, req.ID, tenantID); err != nil {
					return err
				}
				// Auto-close the grouped incident (Fase 3 refino R1): if this alert belongs to a
				// grouped incident and no sibling alert under it remains open, close the incident so
				// resolving alerts one-by-one keeps the incident feed consistent with the alert feed.
				// Only closes when nothing is left open — a still-open sibling keeps the incident open.
				if _, err = tx.Exec(ctx, `
					UPDATE incidents i
					SET status = 'resolved', resolved_at = NOW(), updated_at = NOW()
					WHERE i.tenant_id = $1
					  AND i.status <> 'resolved'
					  AND i.id = (SELECT incident_id FROM alerts WHERE id = $2 AND created_at = $3 AND tenant_id = $1)
					  AND NOT EXISTS (
						SELECT 1 FROM alerts a
						WHERE a.tenant_id = $1 AND a.incident_id = i.id AND a.status <> 'resolved'
					  )
				`, tenantID, req.ID, req.CreatedAt); err != nil {
					return err
				}
				return nil
			}
			return nil
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to resolve incident: %v", err), http.StatusInternalServerError)
			return
		}

		if rowsAffected == 0 {
			http.Error(w, "Incident not found or doesn't belong to tenant", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Incidente resolvido com sucesso"})
	}
}

// severityOrder fixes the display/serialization order of SLASeverityBreakdown entries —
// fatal (most urgent) through info (least), independent of SQL row order.
var severityOrder = []string{"fatal", "critical", "warning", "info"}

type SLASeverityBreakdown struct {
	Severity      string  `json:"severity"`
	TargetMinutes float64 `json:"target_minutes"`
	Count         int     `json:"count"`
	ResolvedCount int     `json:"resolved_count"`
	AverageTTA    float64 `json:"average_tta"`    // minutes; 0 if no acknowledged alerts in this severity
	AverageTTR    float64 `json:"average_ttr"`    // minutes; 0 if no resolved alerts in this severity
	Compliance    float64 `json:"compliance_pct"` // % of resolved alerts in this severity resolved within target
}

type SLAExecutiveStats struct {
	TotalIncidents  int                    `json:"total_incidents"`
	ResolvedCount   int                    `json:"resolved_count"`
	UnresolvedCount int                    `json:"unresolved_count"`
	AverageTTA      float64                `json:"average_tta"`    // minutes, overall
	AverageTTR      float64                `json:"average_ttr"`    // minutes, overall
	SLACompliance   float64                `json:"sla_compliance"` // %, severity-target-based
	BySeverity      []SLASeverityBreakdown `json:"by_severity"`    // always 4 rows: fatal, critical, warning, info
}

// severityRow is the raw per-severity query result, before the pure aggregation step.
type severityRow struct {
	severity      string
	targetMinutes float64
	count         int
	resolvedCount int
	avgTTA        float64
	avgTTR        float64
	metSLACount   int
}

// deriveSLAExecutiveStats aggregates per-severity rows into the final response — pure
// function, no DB dependency, so it's unit-testable without Postgres (see handler_test.go).
// Overall SLA compliance is computed only over already-resolved incidents (an open incident
// hasn't "passed or failed" its target yet), defaulting to 100% when there's no resolved data.
func deriveSLAExecutiveStats(rows []severityRow) SLAExecutiveStats {
	var total, resolved int
	var totalMet, totalResolvedForCompliance int
	bySeverity := make([]SLASeverityBreakdown, 0, len(rows))

	for _, row := range rows {
		total += row.count
		resolved += row.resolvedCount
		totalMet += row.metSLACount
		totalResolvedForCompliance += row.resolvedCount

		compliance := 100.0
		if row.resolvedCount > 0 {
			compliance = float64(row.metSLACount) / float64(row.resolvedCount) * 100.0
		}

		bySeverity = append(bySeverity, SLASeverityBreakdown{
			Severity:      row.severity,
			TargetMinutes: row.targetMinutes,
			Count:         row.count,
			ResolvedCount: row.resolvedCount,
			AverageTTA:    row.avgTTA,
			AverageTTR:    row.avgTTR,
			Compliance:    compliance,
		})
	}

	overallCompliance := 100.0
	if totalResolvedForCompliance > 0 {
		overallCompliance = float64(totalMet) / float64(totalResolvedForCompliance) * 100.0
	}

	var avgTTASum, avgTTRSum float64
	var ttaWeight, ttrWeight int
	for _, row := range rows {
		if row.avgTTA > 0 {
			avgTTASum += row.avgTTA * float64(row.resolvedCount)
			ttaWeight += row.resolvedCount
		}
		if row.avgTTR > 0 {
			avgTTRSum += row.avgTTR * float64(row.resolvedCount)
			ttrWeight += row.resolvedCount
		}
	}
	overallTTA, overallTTR := 0.0, 0.0
	if ttaWeight > 0 {
		overallTTA = avgTTASum / float64(ttaWeight)
	}
	if ttrWeight > 0 {
		overallTTR = avgTTRSum / float64(ttrWeight)
	}

	return SLAExecutiveStats{
		TotalIncidents:  total,
		ResolvedCount:   resolved,
		UnresolvedCount: total - resolved,
		AverageTTA:      overallTTA,
		AverageTTR:      overallTTR,
		SLACompliance:   overallCompliance,
		BySeverity:      bySeverity,
	}
}

// computeSLAExecutiveStats is the single canonical place that computes MTTA/MTTR/SLA-compliance
// for a tenant over the last 30 days. HandleGetSLAReport calls this directly.
func computeSLAExecutiveStats(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (SLAExecutiveStats, error) {
	// Per-tenant SLA targets (tenant_sla overrides merged onto the built-in defaults). The
	// resolution/compliance target is the MTTR target; a severity a tenant hasn't customized falls
	// back to the default.
	effective, err := loadTenantSLATargets(ctx, tx, tenantID)
	if err != nil {
		return SLAExecutiveStats{}, err
	}

	// VALUES-seeded so all four severities always appear in the result (a plain GROUP BY on
	// alerts.severity would silently drop any severity with zero alerts in the window).
	// COUNT(a.id) (not COUNT(*)) is required so unmatched LEFT JOIN rows count as 0, not 1.
	// Targets are parameterized ($2..$5) so they come from the tenant's config, not a hardcode.
	query := `
		WITH severity_targets (severity, target_minutes) AS (
			VALUES ('fatal', $2::numeric), ('critical', $3::numeric), ('warning', $4::numeric), ('info', $5::numeric)
		)
		SELECT
			st.severity,
			st.target_minutes,
			COUNT(a.id) AS total,
			COUNT(a.id) FILTER (WHERE a.status = 'resolved') AS resolved,
			COALESCE(AVG(EXTRACT(EPOCH FROM (a.acknowledged_at - a.created_at)) / 60)
					 FILTER (WHERE a.acknowledged_at IS NOT NULL), 0) AS avg_tta,
			COALESCE(AVG(EXTRACT(EPOCH FROM (a.resolved_at - a.created_at)) / 60)
					 FILTER (WHERE a.resolved_at IS NOT NULL), 0) AS avg_ttr,
			COUNT(a.id) FILTER (
				WHERE a.status = 'resolved'
				  AND a.resolved_at IS NOT NULL
				  AND EXTRACT(EPOCH FROM (a.resolved_at - a.created_at)) / 60 <= st.target_minutes
			) AS met_sla
		FROM severity_targets st
		LEFT JOIN alerts a
			ON a.severity = st.severity
		   AND a.tenant_id = $1
		   AND a.created_at >= NOW() - INTERVAL '30 days'
		GROUP BY st.severity, st.target_minutes
	`
	rows, err := tx.Query(ctx, query, tenantID,
		effective["fatal"].MTTRTargetMinutes,
		effective["critical"].MTTRTargetMinutes,
		effective["warning"].MTTRTargetMinutes,
		effective["info"].MTTRTargetMinutes)
	if err != nil {
		return SLAExecutiveStats{}, err
	}
	defer rows.Close()

	bySeverity := make(map[string]severityRow, 4)
	for rows.Next() {
		var row severityRow
		if err := rows.Scan(&row.severity, &row.targetMinutes, &row.count, &row.resolvedCount, &row.avgTTA, &row.avgTTR, &row.metSLACount); err != nil {
			return SLAExecutiveStats{}, err
		}
		bySeverity[row.severity] = row
	}
	if err := rows.Err(); err != nil {
		return SLAExecutiveStats{}, err
	}

	orderedRows := make([]severityRow, 0, len(severityOrder))
	for _, sev := range severityOrder {
		if row, ok := bySeverity[sev]; ok {
			orderedRows = append(orderedRows, row)
		} else {
			orderedRows = append(orderedRows, severityRow{severity: sev, targetMinutes: effective[sev].MTTRTargetMinutes})
		}
	}

	return deriveSLAExecutiveStats(orderedRows), nil
}

// HandleGetSLAReport calculates MTTA/MTTR/SLA-compliance averages for a tenant, broken down by
// severity, using the tenant's configured SLA targets (see loadTenantSLATargets / tenant_sla).
func HandleGetSLAReport(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var stats SLAExecutiveStats
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var err error
			stats, err = computeSLAExecutiveStats(ctx, tx, tenantID)
			return err
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to calculate SLA report: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

// HandleUptimeKumaIngest ingests and normalizes Uptime Kuma status notifications
func HandleUptimeKumaIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		// Verify active integration setting
		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "uptimekuma").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Uptime Kuma integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceUptimeKuma).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Uptime Kuma JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","uptimekuma"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Uptime Kuma JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue incident", http.StatusInternalServerError)
			return
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","uptimekuma"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","uptimekuma"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "Uptime Kuma alert successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// HandleGrafanaIngest ingests and normalizes Grafana alerts via webhook
func HandleGrafanaIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		// Verify active integration setting
		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "grafana").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Grafana integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceGrafana).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Grafana alert JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","grafana"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Grafana alert JSON payload", http.StatusBadRequest)
			return
		}

		var incidentIDs []uuid.UUID
		var eventBytesList [][]byte

		for _, incident := range incidents {
			incidentIDs = append(incidentIDs, incident.ID)

			bytes, err := json.Marshal(incident)
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			eventBytesList = append(eventBytesList, bytes)
		}

		if len(eventBytesList) > 0 {
			pipe := redisClient.Pipeline()
			for _, bytes := range eventBytesList {
				pipe.LPush(r.Context(), queue.AlertsNormalizedQueueKey, bytes)
			}
			_, err := pipe.Exec(r.Context())
			if err != nil {
				http.Error(w, "Internal Server Error: Failed to queue Grafana alerts", http.StatusInternalServerError)
				return
			}
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","grafana"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","grafana"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestBatchResponse{
			Status:    "accepted",
			IDs:       incidentIDs,
			Message:   fmt.Sprintf("Successfully normalized and queued %d Grafana alerts", len(incidentIDs)),
			Timestamp: time.Now(),
		})
	}
}

// HandleZabbixIngest ingests and normalizes Zabbix problem triggers
func HandleZabbixIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		// Verify active integration setting
		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "zabbix").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Zabbix integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceZabbix).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Zabbix JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","zabbix"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Zabbix JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue Zabbix alert", http.StatusInternalServerError)
			return
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","zabbix"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","zabbix"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "Zabbix trigger event successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// HandleOTLPIngest ingests OTLP/HTTP+JSON log records (ExportLogsServiceRequest), surfacing
// only ERROR/FATAL-severity records as incidents (see internal/connector/otlp.go).
func HandleOTLPIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "otlp").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: OTLP integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceOTLP).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid OTLP JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","otlp"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid OTLP JSON payload", http.StatusBadRequest)
			return
		}

		var incidentIDs []uuid.UUID
		if len(incidents) > 0 {
			pipe := redisClient.Pipeline()
			for _, incident := range incidents {
				incidentIDs = append(incidentIDs, incident.ID)
				bytes, err := json.Marshal(incident)
				if err != nil {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				pipe.LPush(r.Context(), queue.AlertsNormalizedQueueKey, bytes)
			}
			if _, err := pipe.Exec(r.Context()); err != nil {
				http.Error(w, "Internal Server Error: Failed to queue OTLP incidents", http.StatusInternalServerError)
				return
			}
		}

		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","otlp"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","otlp"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestBatchResponse{
			Status:    "accepted",
			IDs:       incidentIDs,
			Message:   fmt.Sprintf("Successfully normalized and queued %d OTLP log-derived alerts", len(incidentIDs)),
			Timestamp: time.Now(),
		})
	}
}

// HandleIcingaIngest ingests Icinga2/Nagios notification-script webhooks (a JSON contract we
// define, since neither tool has a native webhook — see internal/connector/icinga.go).
func HandleIcingaIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "icinga").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Icinga/Nagios integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceIcinga).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Icinga/Nagios JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","icinga"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Icinga/Nagios JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue Icinga/Nagios alert", http.StatusInternalServerError)
			return
		}

		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","icinga"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","icinga"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "Icinga/Nagios notification successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// HandleAzureMonitorIngest ingests Azure Monitor Action Group webhook alerts (Common Alert
// Schema — see internal/connector/azuremonitor.go).
func HandleAzureMonitorIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "azuremonitor").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Azure Monitor integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceAzureMonitor).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Azure Monitor JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","azuremonitor"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Azure Monitor JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue Azure Monitor alert", http.StatusInternalServerError)
			return
		}

		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","azuremonitor"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","azuremonitor"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "Azure Monitor alert successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// HandlePagerDutyIngest ingests PagerDuty Webhooks V3 events (inbound — surfaces PD-native
// incidents as NOC alerts; independent of outbound escalation, see internal/notifier).
func HandlePagerDutyIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "pagerduty").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: PagerDuty integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourcePagerDuty).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid PagerDuty JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","pagerduty"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid PagerDuty JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue PagerDuty alert", http.StatusInternalServerError)
			return
		}

		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","pagerduty"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","pagerduty"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "PagerDuty event successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// HandleOpsgenieIngest ingests Opsgenie's generic Webhook integration payloads (inbound —
// independent of outbound escalation, see internal/notifier).
func HandleOpsgenieIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "opsgenie").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: Opsgenie integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceOpsgenie).MapToUnified(bodyBytes, tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Opsgenie JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","opsgenie"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Opsgenie JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue Opsgenie alert", http.StatusInternalServerError)
			return
		}

		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","opsgenie"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","opsgenie"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "Opsgenie alert successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// handleTypedIngest is the shared flow for typed single-incident ingest endpoints: gate on an
// active tenant_integration → read body → map through the registered connector → queue on the
// normalized queue → refresh heartbeat / clear last error. The older per-source handlers above
// predate this helper and stay inlined; new sources (CrowdStrike/Palo Alto/Fortinet) use it so we
// don't re-duplicate ~50 lines of identical boilerplate each.
func handleTypedIngest(pgPool *pgxpool.Pool, redisClient *redis.Client, source model.IncidentSource, label string) http.HandlerFunc {
	sourceType := string(source)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, sourceType).Scan(&exists)
		if err != nil || !exists {
			http.Error(w, fmt.Sprintf("Forbidden: %s integration not active for this tenant", label), http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(source).MapToUnified(bodyBytes, tenantID)
		if err != nil || len(incidents) == 0 {
			errMsg := fmt.Sprintf("Bad Request: Invalid %s JSON payload: %v", label, err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error",sourceType), errMsg, 24*time.Hour)
			http.Error(w, fmt.Sprintf("Bad Request: Invalid %s JSON payload", label), http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if err := redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err(); err != nil {
			http.Error(w, fmt.Sprintf("Internal Server Error: Failed to queue %s alert", label), http.StatusInternalServerError)
			return
		}

		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat",sourceType), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error",sourceType))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   fmt.Sprintf("%s alert successfully normalized and queued", label),
			Timestamp: incident.Timestamp,
		})
	}
}

// HandleCrowdStrikeIngest ingests CrowdStrike Falcon EDR detections (POSTed by a Falcon Fusion
// workflow HTTP action — see internal/connector/crowdstrike.go).
func HandleCrowdStrikeIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return handleTypedIngest(pgPool, redisClient, model.SourceCrowdStrike, "CrowdStrike")
}

// HandlePaloAltoIngest ingests PAN-OS threat logs forwarded via an HTTP Log Forwarding profile
// (see internal/connector/paloalto.go).
func HandlePaloAltoIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return handleTypedIngest(pgPool, redisClient, model.SourcePaloAlto, "Palo Alto")
}

// HandleFortinetIngest ingests FortiGate UTM/IPS logs posted via a FortiOS Automation Stitch
// webhook (see internal/connector/fortinet.go).
func HandleFortinetIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return handleTypedIngest(pgPool, redisClient, model.SourceFortinet, "Fortinet")
}

type SaveSecretRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// HandleSaveSecret encrypts and stores credentials inside the secure tenant_vault with RLS enforcement
func HandleSaveSecret(pgPool *pgxpool.Pool, vaultRepo repository.VaultRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var req SaveSecretRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: Invalid JSON payload", http.StatusBadRequest)
			return
		}

		if req.Key == "" || req.Value == "" {
			http.Error(w, "Bad Request: key and value are required fields", http.StatusBadRequest)
			return
		}

		masterKey, err := security.GetMasterKey()
		if err != nil {
			log.Printf("Vault Master Key Error: %v", err)
			http.Error(w, "Internal Server Error: Vault encryption setup failure", http.StatusInternalServerError)
			return
		}

		// Encrypt credentials using AES-GCM-256 with the tenant-derived key.
		encrypted, nonce, err := security.EncryptForTenant([]byte(req.Value), masterKey, tenantID)
		if err != nil {
			log.Printf("Encryption Error: %v", err)
			http.Error(w, "Internal Server Error: Encryption failure", http.StatusInternalServerError)
			return
		}

		secret := &model.VaultSecret{
			ID:             uuid.New(),
			TenantID:       tenantID,
			SecretKey:      req.Key,
			EncryptedValue: encrypted,
			Nonce:          nonce,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		tenantCtx := db.WithTenantID(r.Context(), tenantID)
		err = db.ExecuteInTenantTx(tenantCtx, pgPool, func(tx pgx.Tx) error {
			query := `
				INSERT INTO tenant_vault (id, tenant_id, secret_key, encrypted_value, nonce, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (tenant_id, secret_key) 
				DO UPDATE SET encrypted_value = EXCLUDED.encrypted_value, nonce = EXCLUDED.nonce, updated_at = EXCLUDED.updated_at
			`
			_, err := tx.Exec(tenantCtx, query, secret.ID, secret.TenantID, secret.SecretKey, secret.EncryptedValue, secret.Nonce, secret.CreatedAt, secret.UpdatedAt)
			return err
		})

		if err != nil {
			log.Printf("Vault DB Write Error: %v", err)
			http.Error(w, "Internal Server Error: Failed to commit secret to vault", http.StatusInternalServerError)
			return
		}

		var actorID uuid.UUID
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			actorID = claims.UserID
		}
		// Record the key name only — never the secret value.
		audit.Record(tenantCtx, pgPool, audit.Entry{
			TenantID: tenantID, UserID: actorID,
			Action:    "vault.secret.save",
			Resource:  req.Key,
			IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","message":"Credential encrypted and stored in tenant vault securely"}`))
	}
}

// HandleGetActiveUsers lists all currently connected WebSocket user sessions (Admin only)
func HandleGetActiveUsers(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		activeUsers := hub.GetActiveUsers()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(activeUsers)
	}
}

// HandleListAlerts returns the last 100 alerts for a tenant.
func HandleListAlerts(pgPool *pgxpool.Pool, alertRepo repository.AlertRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var alerts []*model.Alert
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var err error
			// Segregated NOC/SOC console: ?domain=noc|soc filters by the alert source's domain.
			if sources, ok := domain.SourcesForDomain(r.URL.Query().Get("domain")); ok {
				alerts, err = alertRepo.ListByDomain(ctx, tx, tenantID, sources, 100, 0)
			} else {
				alerts, err = alertRepo.List(ctx, tx, tenantID, 100, 0)
			}
			return err
		})
		if err != nil {
			log.Printf("[API Error] Failed to list alerts: %v", err)
			http.Error(w, "Internal Server Error: failed to load alerts", http.StatusInternalServerError)
			return
		}

		if alerts == nil {
			alerts = []*model.Alert{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(alerts)
	}
}

// HandleCleanupAlerts deletes all mock/test alerts from the database.
func HandleCleanupAlerts(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var rowsAffected int64
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := `
				DELETE FROM alerts
				WHERE (ai_analysis->>'host' IN ('watchdog', 'azure-sentinel-vm', 'web-server-99', 'db-node-03', 'auth-gateway', 'ad-domain-controller-01', 'simulado') 
				   OR payload->>'host' IN ('watchdog', 'azure-sentinel-vm', 'web-server-99', 'db-node-03', 'auth-gateway', 'ad-domain-controller-01', 'simulado') 
				   OR summary LIKE '%Mock%' 
				   OR summary LIKE '%Simulado%' 
				   OR summary LIKE '%teste%')
				  AND tenant_id = $1
			`
			res, err := tx.Exec(ctx, query, tenantID)
			if err != nil {
				return err
			}
			rowsAffected = res.RowsAffected()
			return nil
		})

		if err != nil {
			log.Printf("[API Error] Failed to cleanup mock alerts for tenant %s: %v", tenantID, err)
			http.Error(w, "Failed to cleanup mock alerts", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "success",
			"rows_affected": rowsAffected,
		})
	}
}

// getTenantWebhookSecret fetches and decrypts the tenant's webhook_hmac_secret from the vault,
// provisioned via HandleGenerateWebhookSecret. Returns an empty string (no error surfaced to
// caller distinctly) if the tenant hasn't configured one yet.
func getTenantWebhookSecret(ctx context.Context, pgPool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID uuid.UUID) (string, error) {
	masterKey, err := security.GetMasterKey()
	if err != nil {
		return "", err
	}

	tenantCtx := db.WithTenantID(ctx, tenantID)
	var secretPlain string
	err = db.ExecuteInTenantTx(tenantCtx, pgPool, func(tx pgx.Tx) error {
		sec, err := vaultRepo.GetSecretByKey(tenantCtx, tx, "webhook_hmac_secret")
		if err != nil {
			return err
		}
		decrypted, err := security.DecryptForTenant(sec.EncryptedValue, sec.Nonce, masterKey, tenantID)
		if err != nil {
			return err
		}
		secretPlain = string(decrypted)
		return nil
	})
	return secretPlain, err
}
