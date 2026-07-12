package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// CrowdStrikeDetection is the CrowdStrike Falcon detection payload. Falcon has no fixed native
// webhook — admins wire a Falcon Fusion workflow with an HTTP action that POSTs this JSON to
// /api/v1/ingest/crowdstrike. Fields mirror the Falcon detection summary; both the numeric
// `severity` (1-100) and the human `severity_name` (Informational..Critical) are accepted,
// preferring the name when present.
type CrowdStrikeDetection struct {
	DetectionID  string `json:"detection_id"`
	Severity     int    `json:"severity"`      // 1-100 numeric scale
	SeverityName string `json:"severity_name"` // Informational, Low, Medium, High, Critical
	Tactic       string `json:"tactic"`
	Technique    string `json:"technique"`
	Filename     string `json:"filename"`
	Description  string `json:"description"`
	Timestamp    string `json:"timestamp"` // RFC3339
	Device       struct {
		Hostname   string `json:"hostname"`
		ExternalIP string `json:"external_ip"`
		LocalIP    string `json:"local_ip"`
	} `json:"device"`
}

type crowdStrikeConnector struct{}

func init() {
	Register(crowdStrikeConnector{})
}

func (crowdStrikeConnector) Type() model.IncidentSource { return model.SourceCrowdStrike }

// mapCrowdStrikeSeverity resolves the unified severity from the human name first, falling back to
// the numeric 1-100 band when the name is absent.
func mapCrowdStrikeSeverity(name string, numeric int) model.AlertSeverity {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "critical":
		return model.SeverityFatal
	case "high":
		return model.SeverityCritical
	case "medium":
		return model.SeverityWarning
	case "low", "informational":
		return model.SeverityInfo
	}
	switch {
	case numeric >= 90:
		return model.SeverityFatal
	case numeric >= 70:
		return model.SeverityCritical
	case numeric >= 40:
		return model.SeverityWarning
	default:
		return model.SeverityInfo
	}
}

func (crowdStrikeConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var det CrowdStrikeDetection
	if err := json.Unmarshal(rawPayload, &det); err != nil {
		return nil, err
	}

	timestamp, err := time.Parse(time.RFC3339, det.Timestamp)
	if err != nil {
		timestamp = time.Now()
	}

	title := det.Technique
	if title == "" {
		title = "CrowdStrike Detection"
	}

	rawMap := map[string]interface{}{
		"tactic":        det.Tactic,
		"technique":     det.Technique,
		"filename":      det.Filename,
		"severity_name": det.SeverityName,
		"severity_num":  det.Severity,
		"external_ip":   det.Device.ExternalIP,
		"local_ip":      det.Device.LocalIP,
	}

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceCrowdStrike,
		ExternalID:  det.DetectionID,
		EventType:   "edr_detection",
		Severity:    mapCrowdStrikeSeverity(det.SeverityName, det.Severity),
		Title:       title,
		Description: det.Description,
		Host:        det.Device.Hostname,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
