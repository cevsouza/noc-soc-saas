package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// AlertmanagerPayload is the batch webhook shape sent by Prometheus Alertmanager (and, per its
// own webhook docs, matched verbatim by Grafana's alerting webhook integration — see grafana.go).
type AlertmanagerPayload struct {
	Receiver          string              `json:"receiver"`
	Status            string              `json:"status"`
	Alerts            []AlertmanagerAlert `json:"alerts"`
	GroupLabels       map[string]string   `json:"groupLabels"`
	CommonLabels      map[string]string   `json:"commonLabels"`
	CommonAnnotations map[string]string   `json:"commonAnnotations"`
	ExternalURL       string              `json:"externalURL"`
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

type prometheusConnector struct{}

func init() {
	Register(prometheusConnector{})
}

func (prometheusConnector) Type() model.IncidentSource { return model.SourcePrometheus }

func (prometheusConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	return mapAlertmanagerBatch(rawPayload, tenantID, model.SourcePrometheus)
}

// mapAlertmanagerBatch handles both shapes Alertmanager-compatible webhooks can arrive in: a
// full batch payload with an "alerts" array, or (as a fallback, matching the previous inline
// behavior in worker.go's mapping engine) a single alert object posted directly. Shared by both
// prometheusConnector and grafanaConnector, which only differ in the Source they stamp.
func mapAlertmanagerBatch(rawPayload []byte, tenantID uuid.UUID, source model.IncidentSource) ([]model.UnifiedIncident, error) {
	var batch AlertmanagerPayload
	if err := json.Unmarshal(rawPayload, &batch); err == nil && len(batch.Alerts) > 0 {
		incidents := make([]model.UnifiedIncident, 0, len(batch.Alerts))
		for _, alert := range batch.Alerts {
			incident := mapAlertmanagerAlert(alert, tenantID)
			incident.Source = source
			incidents = append(incidents, incident)
		}
		return incidents, nil
	}

	var single AlertmanagerAlert
	if err := json.Unmarshal(rawPayload, &single); err != nil {
		return nil, err
	}
	incident := mapAlertmanagerAlert(single, tenantID)
	incident.Source = source
	return []model.UnifiedIncident{incident}, nil
}

func mapAlertmanagerAlert(alert AlertmanagerAlert, tenantID uuid.UUID) model.UnifiedIncident {
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
