package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// PaloAltoThreatLog is a PAN-OS threat/traffic log. PAN-OS forwards logs via an HTTP Log
// Forwarding profile whose payload format the admin defines; this connector expects that profile
// to emit the fields below (documented so admins can build the matching template), since there is
// no universal native webhook JSON. External ID prefers the PAN threat id, falling back to
// source IP + threat name so repeated hits from the same source dedupe stably.
type PaloAltoThreatLog struct {
	Serial        string `json:"serial"`
	Type          string `json:"type"`     // THREAT, TRAFFIC, ...
	ThreatID      string `json:"threatid"` // PAN threat signature id
	ThreatName    string `json:"threat_name"`
	Severity      string `json:"severity"` // informational, low, medium, high, critical
	Action        string `json:"action"`   // alert, deny, drop, reset-both, ...
	SrcIP         string `json:"src_ip"`
	DstIP         string `json:"dst_ip"`
	Rule          string `json:"rule"`
	DeviceName    string `json:"device_name"`
	TimeGenerated string `json:"time_generated"` // "2006/01/02 15:04:05"
}

type paloAltoConnector struct{}

func init() {
	Register(paloAltoConnector{})
}

func (paloAltoConnector) Type() model.IncidentSource { return model.SourcePaloAlto }

// mapFirewallSeverity maps the shared informational/low/medium/high/critical firewall severity
// vocabulary (used by PAN-OS) to the unified scale.
func mapFirewallSeverity(sev string) model.AlertSeverity {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical":
		return model.SeverityFatal
	case "high":
		return model.SeverityCritical
	case "medium":
		return model.SeverityWarning
	default: // low, informational, unknown
		return model.SeverityInfo
	}
}

func (paloAltoConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var logEntry PaloAltoThreatLog
	if err := json.Unmarshal(rawPayload, &logEntry); err != nil {
		return nil, err
	}

	timestamp, err := time.Parse("2006/01/02 15:04:05", logEntry.TimeGenerated)
	if err != nil {
		timestamp = time.Now()
	}

	externalID := logEntry.ThreatID
	if externalID == "" {
		externalID = logEntry.SrcIP + "|" + logEntry.ThreatName
	}

	title := logEntry.ThreatName
	if title == "" {
		title = "Palo Alto Threat"
	}

	rawMap := map[string]interface{}{
		"serial":   logEntry.Serial,
		"type":     logEntry.Type,
		"action":   logEntry.Action,
		"src_ip":   logEntry.SrcIP,
		"dst_ip":   logEntry.DstIP,
		"rule":     logEntry.Rule,
		"threat":   logEntry.ThreatName,
		"threatid": logEntry.ThreatID,
	}

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourcePaloAlto,
		ExternalID:  externalID,
		EventType:   "firewall_threat",
		Severity:    mapFirewallSeverity(logEntry.Severity),
		Title:       title,
		Description: "Action " + logEntry.Action + " on rule " + logEntry.Rule + " (" + logEntry.SrcIP + " → " + logEntry.DstIP + ")",
		Host:        logEntry.DeviceName,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
