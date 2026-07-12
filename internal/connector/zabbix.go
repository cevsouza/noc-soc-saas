package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

type ZabbixPayload struct {
	AlertSubject string `json:"alert_subject"`
	AlertMessage string `json:"alert_message"`
	Host         string `json:"host"`
	Severity     string `json:"severity"`
	TriggerID    string `json:"trigger_id"`
	EventID      string `json:"event_id"`
	EventValue   string `json:"event_value"` // "1" = Problem, "0" = OK
}

type zabbixConnector struct{}

func init() {
	Register(zabbixConnector{})
}

func (zabbixConnector) Type() model.IncidentSource { return model.SourceZabbix }

func (zabbixConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload ZabbixPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}

	severity := model.SeverityInfo
	switch strings.ToLower(payload.Severity) {
	case "warning", "average":
		severity = model.SeverityWarning
	case "high":
		severity = model.SeverityCritical
	case "disaster":
		severity = model.SeverityFatal
	}

	rawMap := make(map[string]interface{})
	rawMap["subject"] = payload.AlertSubject
	rawMap["message"] = payload.AlertMessage
	rawMap["trigger_id"] = payload.TriggerID
	rawMap["event_id"] = payload.EventID
	rawMap["event_value"] = payload.EventValue

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceZabbix,
		ExternalID:  payload.EventID,
		EventType:   "zabbix_trigger",
		Severity:    severity,
		Title:       payload.AlertSubject,
		Description: payload.AlertMessage,
		Host:        payload.Host,
		RawPayload:  rawMap,
		Timestamp:   time.Now(),
	}
	return []model.UnifiedIncident{incident}, nil
}
