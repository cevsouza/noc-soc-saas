package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// IcingaNotificationPayload is a JSON contract WE define for Icinga2 and Nagios, since neither
// tool has a native JSON webhook — both are driven by shell-script notification commands
// (Icinga2's NotificationCommand, Nagios's notify-by-* commands). Admins wire those scripts to
// `curl -X POST` this shape. One connector covers both tools since there's no meaningful
// field-level difference once we own the contract.
type IcingaNotificationPayload struct {
	CheckType        string `json:"check_type"` // "host" | "service"
	HostName         string `json:"host_name"`
	ServiceName      string `json:"service_name,omitempty"`
	State            string `json:"state"`             // OK, WARNING, CRITICAL, UNKNOWN, UP, DOWN
	StateType        string `json:"state_type"`        // HARD, SOFT
	NotificationType string `json:"notification_type"` // PROBLEM, RECOVERY, ACKNOWLEDGEMENT, ...
	Output           string `json:"output"`
	LongDateTime     string `json:"long_date_time"`   // RFC3339
	Source           string `json:"source,omitempty"` // informational: "icinga2" | "nagios"
}

type icingaConnector struct{}

func init() {
	Register(icingaConnector{})
}

func (icingaConnector) Type() model.IncidentSource { return model.SourceIcinga }

func (icingaConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload IcingaNotificationPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}

	severity := model.SeverityInfo
	switch strings.ToUpper(payload.State) {
	case "CRITICAL", "DOWN":
		severity = model.SeverityCritical
	case "WARNING", "UNKNOWN":
		// UNKNOWN is ambiguous (check itself failed) rather than a confirmed problem, but it
		// still needs a human to look — treated as warning, not silently info.
		severity = model.SeverityWarning
	case "OK", "UP":
		severity = model.SeverityInfo
	}

	externalID := payload.HostName
	if payload.ServiceName != "" {
		externalID = payload.HostName + "/" + payload.ServiceName
	}

	timestamp, err := time.Parse(time.RFC3339, payload.LongDateTime)
	if err != nil {
		timestamp = time.Now()
	}

	title := payload.HostName
	if payload.ServiceName != "" {
		title += "/" + payload.ServiceName
	}
	title += " is " + payload.State

	rawMap := make(map[string]interface{})
	rawMap["check_type"] = payload.CheckType
	rawMap["state_type"] = payload.StateType
	rawMap["notification_type"] = payload.NotificationType
	rawMap["source"] = payload.Source

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceIcinga,
		ExternalID:  externalID,
		EventType:   "icinga_" + strings.ToLower(payload.CheckType) + "_check",
		Severity:    severity,
		Title:       title,
		Description: payload.Output,
		Host:        payload.HostName,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
