package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// OpsgenieWebhookPayload is Opsgenie's generic "Webhook" integration payload shape (inbound:
// surfaces alerts already created in Opsgenie as NOC alerts). Independent of the outbound
// escalation path in internal/notifier/opsgenie.go, which calls Opsgenie's Alert API v2 to
// page out when one of *our* alerts goes critical/fatal.
type OpsgenieWebhookPayload struct {
	Action string `json:"action"` // Create, Close, Acknowledge, ...
	Alert  struct {
		AlertID  string   `json:"alertId"`
		Message  string   `json:"message"`
		Source   string   `json:"source"`
		Entity   string   `json:"entity"`
		Priority string   `json:"priority"` // "P1".."P5"
		Tags     []string `json:"tags"`
	} `json:"alert"`
}

type opsgenieConnector struct{}

func init() {
	Register(opsgenieConnector{})
}

func (opsgenieConnector) Type() model.IncidentSource { return model.SourceOpsgenie }

func (opsgenieConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload OpsgenieWebhookPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}

	rawMap := make(map[string]interface{})
	rawMap["action"] = payload.Action
	rawMap["source"] = payload.Alert.Source
	rawMap["tags"] = payload.Alert.Tags

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceOpsgenie,
		ExternalID:  payload.Alert.AlertID,
		EventType:   "opsgenie_" + strings.ToLower(payload.Action),
		Severity:    opsgeniePrioritySeverity(payload.Alert.Priority),
		Title:       payload.Alert.Message,
		Description: payload.Alert.Message,
		Host:        payload.Alert.Entity,
		RawPayload:  rawMap,
		// Opsgenie's generic webhook payload doesn't reliably carry a top-level timestamp
		// across all action types (Create/Close/Acknowledge/...), unlike every other source.
		Timestamp: time.Now(),
	}
	return []model.UnifiedIncident{incident}, nil
}

// opsgeniePrioritySeverity maps Opsgenie/PagerDuty's shared "P1".."P5" priority convention to
// model.AlertSeverity. Shared by opsgenieConnector and pagerDutyConnector's Priority fallback.
func opsgeniePrioritySeverity(priority string) model.AlertSeverity {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case "P1":
		return model.SeverityFatal
	case "P2":
		return model.SeverityCritical
	case "P3":
		return model.SeverityWarning
	case "P4", "P5":
		return model.SeverityInfo
	default:
		return model.SeverityInfo
	}
}
