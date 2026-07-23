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

	// Aliases accepted from the stock Zabbix webhook media type, whose default parameter names
	// are Subject/Message/To. Many installs paste our ingest URL but never rename the parameters,
	// which used to produce an empty, unreadable alert. Accepting the aliases makes that setup
	// degrade gracefully instead of silently losing the content.
	Subject string `json:"Subject"`
	Message string `json:"Message"`
}

// maxDerivedTitleLen caps a title derived from a free-text message body so the alert list stays
// readable (Zabbix default message templates are multi-line and can be long).
const maxDerivedTitleLen = 120

// fallbackZabbixTitle is the last resort when a payload carries no usable text at all, so the
// cockpit never renders a blank row.
const fallbackZabbixTitle = "Evento Zabbix (sem descrição)"

// firstMeaningfulLine returns the first non-empty trimmed line of s, capped at maxLen runes.
func firstMeaningfulLine(s string, maxLen int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := []rune(line)
		if len(r) > maxLen {
			return string(r[:maxLen]) + "…"
		}
		return line
	}
	return ""
}

// extractLabeled pulls the value of a "Label: value" line out of a multi-line body. Zabbix's
// default message templates emit exactly this shape ("Host: web-01", "Severity: High"), so it is
// a precise recovery path — deliberately NOT a loose keyword scan, which would misread a phrase
// like "CPU load is too high" as a High severity.
func extractLabeled(text, label string) string {
	want := strings.ToLower(label) + ":"
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) <= len(want) {
			continue
		}
		if strings.EqualFold(trimmed[:len(want)], want) {
			return strings.TrimSpace(trimmed[len(want):])
		}
	}
	return ""
}

// resolveZabbixPayload fills the structured fields from looser aliases and from the labeled lines
// of the message body, so a webhook left with Zabbix's default parameter names still yields a
// readable, correctly-classified alert. Pure function — unit tested without network or DB.
func resolveZabbixPayload(p ZabbixPayload) ZabbixPayload {
	if p.AlertMessage == "" {
		p.AlertMessage = p.Message
	}
	if p.AlertSubject == "" {
		p.AlertSubject = p.Subject
	}
	// Still no subject: derive one from the message body rather than showing an empty title.
	if p.AlertSubject == "" {
		p.AlertSubject = firstMeaningfulLine(p.AlertMessage, maxDerivedTitleLen)
	}
	if p.AlertSubject == "" {
		p.AlertSubject = fallbackZabbixTitle
	}
	// Recover host/severity only from explicit labeled lines — never guess from prose.
	if p.Host == "" {
		p.Host = extractLabeled(p.AlertMessage, "host")
	}
	if p.Severity == "" {
		p.Severity = extractLabeled(p.AlertMessage, "severity")
	}
	if p.EventID == "" {
		p.EventID = extractLabeled(p.AlertMessage, "event id")
	}
	return p
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
	payload = resolveZabbixPayload(payload)

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
