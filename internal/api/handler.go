package api

import (
	"bytes"
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

	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"noc-api/internal/repository"
	"noc-api/internal/security"
	"noc-api/internal/ws"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type IngestEventRequest struct {
	DeviceID  *uuid.UUID          `json:"device_id,omitempty"`
	EventType string              `json:"event_type"`
	Severity  model.AlertSeverity `json:"severity"`
	Summary   string              `json:"summary"`
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

// Structures for Prometheus Alertmanager Webhook Payload
type AlertmanagerPayload struct {
	Receiver          string               `json:"receiver"`
	Status            string               `json:"status"`
	Alerts            []AlertmanagerAlert  `json:"alerts"`
	GroupLabels       map[string]string    `json:"groupLabels"`
	CommonLabels      map[string]string    `json:"commonLabels"`
	CommonAnnotations map[string]string    `json:"commonAnnotations"`
	ExternalURL       string               `json:"externalURL"`
}

type AlertmanagerAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// Structures for Wazuh Alert Webhook Payload
type WazuhAlertPayload struct {
	Timestamp string `json:"timestamp"`
	Rule      struct {
		Level   int      `json:"level"`
		Comment string   `json:"comment"`
		Sid     int      `json:"sid"`
		ID      string   `json:"id"`
		Groups  []string `json:"groups"`
	} `json:"rule"`
	Agent struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		IP   string `json:"ip"`
	} `json:"agent"`
	Location string                 `json:"location"`
	FullLog  string                 `json:"full_log"`
	Id       string                 `json:"id"` // optional
}

type RawWebhookMessage struct {
	TenantID        uuid.UUID              `json:"tenant_id"`
	IntegrationType string                 `json:"integration_type"`
	Payload         map[string]interface{} `json:"payload"`
}

const (
	AlertsQueueKey           = "noc:queue:alerts"
	AlertsRawQueueKey        = "noc:queue:alerts:raw"
	AlertsNormalizedQueueKey = "noc:queue:alerts:normalized"
	AlertsDLQQueueKey        = "noc:queue:alerts:dlq"
)

// HandleGenericWebhook handles POST /api/v1/webhook/{integration_type}/{tenant_id}
func HandleGenericWebhook(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
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
		if xSignature != "" {
			mac := hmac.New(sha256.New, []byte("itfacil_super_secret_signing_key_9988"))
			mac.Write(bodyBytes)
			expected := hex.EncodeToString(mac.Sum(nil))
			if !hmac.Equal([]byte(expected), []byte(xSignature)) {
				errMsg := "Unauthorized: HMAC signature verification failed"
				redisClient.Set(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), integrationType), errMsg, 24*time.Hour)
				http.Error(w, errMsg, http.StatusUnauthorized)
				return
			}
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid JSON body: %v", err)
			redisClient.Set(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), integrationType), errMsg, 24*time.Hour)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}

		rawMsg := RawWebhookMessage{
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
		redisClient.Set(ctx, fmt.Sprintf("heartbeat:connector:%s:%s", tenantID.String(), integrationType), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(ctx, fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), integrationType))

		// Push to alerts.raw queue
		err = redisClient.LPush(ctx, AlertsRawQueueKey, rawMsgBytes).Err()
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

		err = redisClient.LPush(r.Context(), AlertsQueueKey, eventBytes).Err()
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

		var payload AlertmanagerPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Alertmanager JSON payload: %v", err)
			redisClient.Set(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "prometheus"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Alertmanager JSON payload", http.StatusBadRequest)
			return
		}

		var incidentIDs []uuid.UUID
		var eventBytesList [][]byte

		for _, alert := range payload.Alerts {
			incident := MapPrometheusToUnified(alert, tenantID)
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
				pipe.LPush(r.Context(), AlertsQueueKey, bytes)
			}
			_, err := pipe.Exec(r.Context())
			if err != nil {
				http.Error(w, "Internal Server Error: Failed to queue alerts", http.StatusInternalServerError)
				return
			}
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), fmt.Sprintf("heartbeat:connector:%s:%s", tenantID.String(), "prometheus"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "prometheus"))

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

		var payload WazuhAlertPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Wazuh JSON payload: %v", err)
			redisClient.Set(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "wazuh"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Wazuh JSON payload", http.StatusBadRequest)
			return
		}

		incident := MapWazuhToUnified(payload, tenantID)

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), AlertsQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue alert", http.StatusInternalServerError)
			return
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), fmt.Sprintf("heartbeat:connector:%s:%s", tenantID.String(), "wazuh"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "wazuh"))

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

func MapPrometheusToUnified(alert AlertmanagerAlert, tenantID uuid.UUID) model.UnifiedIncident {
	severity := model.SeverityInfo
	if sevLabel, ok := alert.Labels["severity"]; ok {
		switch strings.ToLower(sevLabel) {
		case "fatal":
			severity = model.SeverityFatal
		case "critical":
			severity = model.SeverityCritical
		case "warning", "warn":
			severity = model.SeverityWarning
		case "info", "debug":
			severity = model.SeverityInfo
		}
	}

	title := alert.Annotations["summary"]
	if title == "" {
		title = alert.Labels["alertname"]
	}

	desc := alert.Annotations["description"]
	if desc == "" {
		desc = alert.Annotations["summary"]
	}

	rawPayload := make(map[string]interface{})
	rawPayload["labels"] = alert.Labels
	rawPayload["annotations"] = alert.Annotations
	rawPayload["generatorURL"] = alert.GeneratorURL
	rawPayload["status"] = alert.Status

	timestamp := alert.StartsAt
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	return model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourcePrometheus,
		ExternalID:  alert.Fingerprint,
		EventType:   alert.Labels["alertname"],
		Severity:    severity,
		Title:       title,
		Description: desc,
		Host:        alert.Labels["instance"],
		RawPayload:  rawPayload,
		Timestamp:   timestamp,
	}
}

func MapWazuhToUnified(alert WazuhAlertPayload, tenantID uuid.UUID) model.UnifiedIncident {
	severity := model.SeverityInfo
	level := alert.Rule.Level
	if level >= 12 {
		severity = model.SeverityFatal
	} else if level >= 8 {
		severity = model.SeverityCritical
	} else if level >= 4 {
		severity = model.SeverityWarning
	}

	eventType := "wazuh_security_event"
	if len(alert.Rule.Groups) > 0 {
		eventType = alert.Rule.Groups[0]
	}

	rawPayload := make(map[string]interface{})
	rawPayload["rule"] = alert.Rule
	rawPayload["agent"] = alert.Agent
	rawPayload["location"] = alert.Location
	rawPayload["full_log"] = alert.FullLog

	timestamp, err := time.Parse(time.RFC3339, alert.Timestamp)
	if err != nil {
		timestamp = time.Now()
	}

	externalID := alert.Id
	if externalID == "" {
		externalID = fmt.Sprintf("%s_%d", alert.Rule.ID, timestamp.Unix())
	}

	host := alert.Agent.IP
	if host == "" {
		host = alert.Agent.Name
	}

	return model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceWazuh,
		ExternalID:  externalID,
		EventType:   eventType,
		Severity:    severity,
		Title:       alert.Rule.Comment,
		Description: alert.FullLog,
		Host:        host,
		RawPayload:  rawPayload,
		Timestamp:   timestamp,
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

// Helper to resolve tenant ID (supports query override for global operators)
func resolveTenantID(r *http.Request) (uuid.UUID, bool) {
	if tenantParam := r.URL.Query().Get("tenant_id"); tenantParam != "" {
		if id, err := uuid.Parse(tenantParam); err == nil {
			return id, true
		}
	}
	return db.TenantIDFromContext(r.Context())
}

// HandleAcknowledgeIncident marks an alert/incident as acknowledged and logs the operator action
func HandleAcknowledgeIncident(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req IncidentAcknowledgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		var rowsAffected int64
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
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
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req IncidentResolveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		var rowsAffected int64
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
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
				_, err = tx.Exec(ctx, logQuery, req.ID, tenantID)
				return err
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

type SLAReportResponse struct {
	TotalIncidents  int     `json:"total_incidents"`
	ResolvedCount   int     `json:"resolved_count"`
	UnresolvedCount int     `json:"unresolved_count"`
	AverageTTA      float64 `json:"average_tta"` // in minutes
	AverageTTR      float64 `json:"average_ttr"` // in minutes
	SLACompliance   float64 `json:"sla_compliance"` // percentage
}

// HandleGetSLAReport calculates averages of response time and resolution time for a tenant.
func HandleGetSLAReport(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var total, resolved, unresolved int
		var avgTTA, avgTTR float64
		var totalDevices, onlineDevices int

		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// 1. Fetch SLA statistics from database
			queryStats := `
				SELECT 
					COUNT(*) as total,
					COUNT(CASE WHEN status = 'resolved' THEN 1 END) as resolved,
					COUNT(CASE WHEN status != 'resolved' THEN 1 END) as unresolved,
					COALESCE(AVG(EXTRACT(EPOCH FROM (acknowledged_at - created_at)) / 60), 0) as avg_tta,
					COALESCE(AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)) / 60), 0) as avg_ttr
				FROM alerts
				WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
			`
			err := tx.QueryRow(ctx, queryStats, tenantID).Scan(&total, &resolved, &unresolved, &avgTTA, &avgTTR)
			if err != nil {
				return err
			}

			// 2. Fetch device availability SLA compliance
			queryDevices := `
				SELECT COUNT(*) as total_devices,
				       COUNT(CASE WHEN status = 'online' THEN 1 END) as online_devices
				FROM devices
				WHERE tenant_id = $1
			`
			_ = tx.QueryRow(ctx, queryDevices, tenantID).Scan(&totalDevices, &onlineDevices)
			return nil
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to calculate SLA report: %v", err), http.StatusInternalServerError)
			return
		}

		slaCompliance := 100.0
		if totalDevices > 0 {
			slaCompliance = (float64(onlineDevices) / float64(totalDevices)) * 100.0
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(SLAReportResponse{
			TotalIncidents:  total,
			ResolvedCount:   resolved,
			UnresolvedCount: unresolved,
			AverageTTA:      avgTTA,
			AverageTTR:      avgTTR,
			SLACompliance:   slaCompliance,
		})
	}
}

// Uptime Kuma Webhook Structures
type UptimeKumaPayload struct {
	Heartbeat struct {
		MonitorID int    `json:"monitorID"`
		Status    int    `json:"status"` // 0 = Down, 1 = Up
		Time      string `json:"time"`
		Msg       string `json:"msg"`
	} `json:"heartbeat"`
	Monitor struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		Url      string `json:"url"`
		Type     string `json:"type"`
	} `json:"monitor"`
	Msg string `json:"msg"`
}

// Zabbix Webhook Structures
type ZabbixPayload struct {
	AlertSubject string `json:"alert_subject"`
	AlertMessage string `json:"alert_message"`
	Host         string `json:"host"`
	Severity     string `json:"severity"`
	TriggerID    string `json:"trigger_id"`
	EventID      string `json:"event_id"`
	EventValue   string `json:"event_value"` // "1" = Problem, "0" = OK
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

		var payload UptimeKumaPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Uptime Kuma JSON payload: %v", err)
			redisClient.Set(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "uptimekuma"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Uptime Kuma JSON payload", http.StatusBadRequest)
			return
		}

		incident := MapUptimeKumaToUnified(payload, tenantID)

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), AlertsQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue incident", http.StatusInternalServerError)
			return
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), fmt.Sprintf("heartbeat:connector:%s:%s", tenantID.String(), "uptimekuma"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "uptimekuma"))

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

		var payload AlertmanagerPayload // Grafana alert format matches Prometheus Alertmanager structure
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Grafana alert JSON payload: %v", err)
			redisClient.Set(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "grafana"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Grafana alert JSON payload", http.StatusBadRequest)
			return
		}

		var incidentIDs []uuid.UUID
		var eventBytesList [][]byte

		for _, alert := range payload.Alerts {
			incident := MapGrafanaToUnified(alert, tenantID)
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
				pipe.LPush(r.Context(), AlertsQueueKey, bytes)
			}
			_, err := pipe.Exec(r.Context())
			if err != nil {
				http.Error(w, "Internal Server Error: Failed to queue Grafana alerts", http.StatusInternalServerError)
				return
			}
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), fmt.Sprintf("heartbeat:connector:%s:%s", tenantID.String(), "grafana"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "grafana"))

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

		var payload ZabbixPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid Zabbix JSON payload: %v", err)
			redisClient.Set(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "zabbix"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid Zabbix JSON payload", http.StatusBadRequest)
			return
		}

		incident := MapZabbixToUnified(payload, tenantID)

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		err = redisClient.LPush(r.Context(), AlertsQueueKey, eventBytes).Err()
		if err != nil {
			http.Error(w, "Internal Server Error: Failed to queue Zabbix alert", http.StatusInternalServerError)
			return
		}

		// Register heartbeat in Redis and clear any previous errors
		redisClient.Set(r.Context(), fmt.Sprintf("heartbeat:connector:%s:%s", tenantID.String(), "zabbix"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), fmt.Sprintf("webhook:error:%s:%s", tenantID.String(), "zabbix"))

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

func MapUptimeKumaToUnified(payload UptimeKumaPayload, tenantID uuid.UUID) model.UnifiedIncident {
	severity := model.SeverityInfo
	if payload.Heartbeat.Status == 0 {
		severity = model.SeverityCritical
	}

	rawPayload := make(map[string]interface{})
	rawPayload["heartbeat"] = payload.Heartbeat
	rawPayload["monitor"] = payload.Monitor
	rawPayload["msg"] = payload.Msg

	// Timestamp layout parsing
	timestamp, err := time.Parse("2006-01-02 15:04:05.000", payload.Heartbeat.Time)
	if err != nil {
		timestamp = time.Now()
	}

	host := payload.Monitor.Hostname
	if host == "" {
		host = payload.Monitor.Url
	}

	return model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceUptimeKuma,
		ExternalID:  fmt.Sprintf("%d_%d", payload.Monitor.ID, timestamp.Unix()),
		EventType:   payload.Monitor.Type,
		Severity:    severity,
		Title:       payload.Msg,
		Description: payload.Heartbeat.Msg,
		Host:        host,
		RawPayload:  rawPayload,
		Timestamp:   timestamp,
	}
}

func MapGrafanaToUnified(alert AlertmanagerAlert, tenantID uuid.UUID) model.UnifiedIncident {
	incident := MapPrometheusToUnified(alert, tenantID)
	incident.Source = model.SourceGrafana
	return incident
}

func MapZabbixToUnified(payload ZabbixPayload, tenantID uuid.UUID) model.UnifiedIncident {
	severity := model.SeverityInfo
	switch strings.ToLower(payload.Severity) {
	case "warning", "average":
		severity = model.SeverityWarning
	case "high":
		severity = model.SeverityCritical
	case "disaster":
		severity = model.SeverityFatal
	}

	rawPayload := make(map[string]interface{})
	rawPayload["subject"] = payload.AlertSubject
	rawPayload["message"] = payload.AlertMessage
	rawPayload["trigger_id"] = payload.TriggerID
	rawPayload["event_id"] = payload.EventID
	rawPayload["event_value"] = payload.EventValue

	return model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceZabbix,
		ExternalID:  payload.EventID,
		EventType:   "zabbix_trigger",
		Severity:    severity,
		Title:       payload.AlertSubject,
		Description: payload.AlertMessage,
		Host:        payload.Host,
		RawPayload:  rawPayload,
		Timestamp:   time.Now(),
	}
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
			token := r.URL.Query().Get("token")
			if token == "" {
				http.Error(w, "Unauthorized: Missing token", http.StatusUnauthorized)
				return
			}
			parsedUUID, err := uuid.Parse(token)
			if err != nil {
				http.Error(w, "Unauthorized: Invalid token format", http.StatusUnauthorized)
				return
			}
			tenantID = parsedUUID
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

		// Encrypt credentials using AES-GCM-256
		encrypted, nonce, err := security.Encrypt([]byte(req.Value), masterKey)
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
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var alerts []*model.Alert
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var err error
			alerts, err = alertRepo.List(ctx, tx, 100, 0)
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

		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var rowsAffected int64
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := `
				DELETE FROM alerts 
				WHERE (ai_analysis->>'host' IN ('watchdog', 'azure-sentinel-vm', 'web-server-99', 'simulado') 
				   OR payload->>'host' IN ('watchdog', 'azure-sentinel-vm', 'web-server-99', 'simulado') 
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



