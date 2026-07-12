package connector

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// FortinetLog is a FortiGate (FortiOS) UTM/IPS log. FortiGate emits these via an Automation
// Stitch with a Webhook action posting JSON; the fields below follow FortiOS log naming. External
// ID prefers the FortiOS `logid`, falling back to source IP + attack name for stable dedupe.
type FortinetLog struct {
	Type      string `json:"type"`    // utm, event, traffic
	Subtype   string `json:"subtype"` // ips, virus, webfilter, ...
	Level     string `json:"level"`   // emergency, alert, critical, error, warning, notification, information, debug
	LogID     string `json:"logid"`
	Attack    string `json:"attack"`
	Msg       string `json:"msg"`
	Action    string `json:"action"`
	SrcIP     string `json:"srcip"`
	DstIP     string `json:"dstip"`
	DevName   string `json:"devname"`
	EventTime string `json:"eventtime"` // unix seconds (string) or RFC3339
}

type fortinetConnector struct{}

func init() {
	Register(fortinetConnector{})
}

func (fortinetConnector) Type() model.IncidentSource { return model.SourceFortinet }

// mapFortinetLevel maps the FortiOS syslog-style level vocabulary to the unified scale.
func mapFortinetLevel(level string) model.AlertSeverity {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "emergency", "alert":
		return model.SeverityFatal
	case "critical", "error":
		return model.SeverityCritical
	case "warning":
		return model.SeverityWarning
	default: // notification, information, debug, unknown
		return model.SeverityInfo
	}
}

func (fortinetConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var logEntry FortinetLog
	if err := json.Unmarshal(rawPayload, &logEntry); err != nil {
		return nil, err
	}

	// FortiOS eventtime is typically unix seconds as a string; accept RFC3339 too.
	timestamp := time.Now()
	if secs, err := strconv.ParseInt(logEntry.EventTime, 10, 64); err == nil && secs > 0 {
		timestamp = time.Unix(secs, 0).UTC()
	} else if parsed, perr := time.Parse(time.RFC3339, logEntry.EventTime); perr == nil {
		timestamp = parsed
	}

	externalID := logEntry.LogID
	if externalID == "" {
		externalID = logEntry.SrcIP + "|" + logEntry.Attack
	}

	title := logEntry.Attack
	if title == "" {
		title = "Fortinet " + logEntry.Subtype + " event"
	}

	description := logEntry.Msg
	if description == "" {
		description = "Action " + logEntry.Action + " (" + logEntry.SrcIP + " → " + logEntry.DstIP + ")"
	}

	rawMap := map[string]interface{}{
		"type":    logEntry.Type,
		"subtype": logEntry.Subtype,
		"level":   logEntry.Level,
		"logid":   logEntry.LogID,
		"action":  logEntry.Action,
		"src_ip":  logEntry.SrcIP,
		"dst_ip":  logEntry.DstIP,
		"attack":  logEntry.Attack,
	}

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceFortinet,
		ExternalID:  externalID,
		EventType:   "firewall_threat",
		Severity:    mapFortinetLevel(logEntry.Level),
		Title:       title,
		Description: description,
		Host:        logEntry.DevName,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
