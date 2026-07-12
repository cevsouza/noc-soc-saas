package connector

import (
	"encoding/json"
	"fmt"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

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
	Location string `json:"location"`
	FullLog  string `json:"full_log"`
	Id       string `json:"id"` // optional
}

type wazuhConnector struct{}

func init() {
	Register(wazuhConnector{})
}

func (wazuhConnector) Type() model.IncidentSource { return model.SourceWazuh }

func (wazuhConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload WazuhAlertPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}

	severity := model.SeverityInfo
	level := payload.Rule.Level
	if level >= 12 {
		severity = model.SeverityFatal
	} else if level >= 8 {
		severity = model.SeverityCritical
	} else if level >= 4 {
		severity = model.SeverityWarning
	}

	eventType := "wazuh_security_event"
	if len(payload.Rule.Groups) > 0 {
		eventType = payload.Rule.Groups[0]
	}

	rawMap := make(map[string]interface{})
	rawMap["rule"] = payload.Rule
	rawMap["agent"] = payload.Agent
	rawMap["location"] = payload.Location
	rawMap["full_log"] = payload.FullLog

	timestamp, err := time.Parse(time.RFC3339, payload.Timestamp)
	if err != nil {
		timestamp = time.Now()
	}

	externalID := payload.Id
	if externalID == "" {
		externalID = fmt.Sprintf("%s_%d", payload.Rule.ID, timestamp.Unix())
	}

	host := payload.Agent.IP
	if host == "" {
		host = payload.Agent.Name
	}

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceWazuh,
		ExternalID:  externalID,
		EventType:   eventType,
		Severity:    severity,
		Title:       payload.Rule.Comment,
		Description: payload.FullLog,
		Host:        host,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
