package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// PagerDutyWebhookPayload is PagerDuty's Webhooks V3 event shape (inbound: surfaces incidents
// already created/updated in PagerDuty as NOC alerts). This is independent of the outbound
// escalation path in internal/notifier/pagerduty.go, which calls PD's Events API v2 to page
// out when one of *our* alerts goes critical/fatal.
type PagerDutyWebhookPayload struct {
	Event struct {
		EventType  string `json:"event_type"` // incident.triggered, .acknowledged, .resolved, ...
		OccurredAt string `json:"occurred_at"`
		Data       struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Status   string `json:"status"`
			Urgency  string `json:"urgency"` // high | low
			Priority *struct {
				Summary string `json:"summary"` // "P1".."P5"
			} `json:"priority"`
		} `json:"data"`
	} `json:"event"`
}

type pagerDutyConnector struct{}

func init() {
	Register(pagerDutyConnector{})
}

func (pagerDutyConnector) Type() model.IncidentSource { return model.SourcePagerDuty }

func (pagerDutyConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload PagerDutyWebhookPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}
	ev := payload.Event

	// Prefer Urgency: present on effectively every PD v3 incident event. Priority is an
	// optional per-account feature, used only as a fallback when Urgency is empty.
	severity := model.SeverityInfo
	switch strings.ToLower(ev.Data.Urgency) {
	case "high":
		severity = model.SeverityCritical
	case "low":
		severity = model.SeverityWarning
	default:
		if ev.Data.Priority != nil {
			severity = opsgeniePrioritySeverity(ev.Data.Priority.Summary)
		}
	}

	timestamp, err := time.Parse(time.RFC3339, ev.OccurredAt)
	if err != nil {
		timestamp = time.Now()
	}

	rawMap := make(map[string]interface{})
	rawMap["event_type"] = ev.EventType
	rawMap["status"] = ev.Data.Status
	rawMap["urgency"] = ev.Data.Urgency

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourcePagerDuty,
		ExternalID:  ev.Data.ID,
		EventType:   "pagerduty_" + strings.TrimPrefix(ev.EventType, "incident."),
		Severity:    severity,
		Title:       ev.Data.Title,
		Description: ev.Data.Title,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
