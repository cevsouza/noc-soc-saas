package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
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

const AlertsQueueKey = "noc:queue:alerts"

// HandleIngest handles generic alert ingestion
func HandleIngest(redisClient *redis.Client) http.HandlerFunc {
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
func HandlePrometheusIngest(redisClient *redis.Client) http.HandlerFunc {
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

		var payload AlertmanagerPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad Request: Invalid Alertmanager JSON payload", http.StatusBadRequest)
			return
		}

		var incidentIDs []uuid.UUID
		var eventBytesList [][]byte

		for _, alert := range payload.Alerts {
			incident := mapPrometheusToUnified(alert, tenantID)
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
func HandleWazuhIngest(redisClient *redis.Client) http.HandlerFunc {
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

		var payload WazuhAlertPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad Request: Invalid Wazuh JSON payload", http.StatusBadRequest)
			return
		}

		incident := mapWazuhToUnified(payload, tenantID)

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

func mapPrometheusToUnified(alert AlertmanagerAlert, tenantID uuid.UUID) model.UnifiedIncident {
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

func mapWazuhToUnified(alert WazuhAlertPayload, tenantID uuid.UUID) model.UnifiedIncident {
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
func HandleDownloadSLAReport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tenantID uuid.UUID
		if tID, ok := db.TenantIDFromContext(r.Context()); ok {
			tenantID = tID
		} else {
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
func HandleUptimeKumaIngest(redisClient *redis.Client) http.HandlerFunc {
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

		var payload UptimeKumaPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad Request: Invalid Uptime Kuma JSON payload", http.StatusBadRequest)
			return
		}

		incident := mapUptimeKumaToUnified(payload, tenantID)

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
func HandleGrafanaIngest(redisClient *redis.Client) http.HandlerFunc {
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

		var payload AlertmanagerPayload // Grafana alert format matches Prometheus Alertmanager structure
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad Request: Invalid Grafana alert JSON payload", http.StatusBadRequest)
			return
		}

		var incidentIDs []uuid.UUID
		var eventBytesList [][]byte

		for _, alert := range payload.Alerts {
			incident := mapGrafanaToUnified(alert, tenantID)
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
func HandleZabbixIngest(redisClient *redis.Client) http.HandlerFunc {
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

		var payload ZabbixPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad Request: Invalid Zabbix JSON payload", http.StatusBadRequest)
			return
		}

		incident := mapZabbixToUnified(payload, tenantID)

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

func mapUptimeKumaToUnified(payload UptimeKumaPayload, tenantID uuid.UUID) model.UnifiedIncident {
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

func mapGrafanaToUnified(alert AlertmanagerAlert, tenantID uuid.UUID) model.UnifiedIncident {
	incident := mapPrometheusToUnified(alert, tenantID)
	incident.Source = model.SourceGrafana
	return incident
}

func mapZabbixToUnified(payload ZabbixPayload, tenantID uuid.UUID) model.UnifiedIncident {
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

