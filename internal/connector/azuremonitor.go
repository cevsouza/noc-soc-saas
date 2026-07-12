package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// AzureMonitorPayload follows Azure Monitor's "Common Alert Schema", delivered by an Action
// Group's Webhook action — push-based, no Azure AD OAuth needed for inbound (unlike the
// Sentinel poll connector, which authenticates outbound to pull incidents).
type AzureMonitorPayload struct {
	SchemaID string `json:"schemaId"`
	Data     struct {
		Essentials struct {
			AlertID           string   `json:"alertId"`
			OriginAlertID     string   `json:"originAlertId"`
			AlertRule         string   `json:"alertRule"`
			Severity          string   `json:"severity"` // "Sev0".."Sev4"
			SignalType        string   `json:"signalType"`
			MonitorCondition  string   `json:"monitorCondition"` // Fired, Resolved
			MonitoringService string   `json:"monitoringService"`
			AlertTargetIDs    []string `json:"alertTargetIDs"`
			FiredDateTime     string   `json:"firedDateTime"`
			ResolvedDateTime  string   `json:"resolvedDateTime"`
			Description       string   `json:"description"`
		} `json:"essentials"`
		AlertContext map[string]interface{} `json:"alertContext"`
	} `json:"data"`
}

type azureMonitorConnector struct{}

func init() {
	Register(azureMonitorConnector{})
}

func (azureMonitorConnector) Type() model.IncidentSource { return model.SourceAzureMonitor }

func (azureMonitorConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload AzureMonitorPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}
	ess := payload.Data.Essentials

	severity := model.SeverityInfo
	switch ess.Severity {
	case "Sev0":
		severity = model.SeverityFatal
	case "Sev1":
		severity = model.SeverityCritical
	case "Sev2":
		severity = model.SeverityWarning
	case "Sev3", "Sev4":
		severity = model.SeverityInfo
	}

	externalID := ess.OriginAlertID
	if externalID == "" {
		externalID = ess.AlertID
	}

	host := ""
	if len(ess.AlertTargetIDs) > 0 {
		host = ess.AlertTargetIDs[0]
	}

	timestampStr := ess.FiredDateTime
	if strings.EqualFold(ess.MonitorCondition, "Resolved") && ess.ResolvedDateTime != "" {
		timestampStr = ess.ResolvedDateTime
	}
	timestamp, err := time.Parse(time.RFC3339, timestampStr)
	if err != nil {
		timestamp = time.Now()
	}

	rawMap := make(map[string]interface{})
	rawMap["signal_type"] = ess.SignalType
	rawMap["monitoring_service"] = ess.MonitoringService
	rawMap["monitor_condition"] = ess.MonitorCondition
	rawMap["alert_context"] = payload.Data.AlertContext

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceAzureMonitor,
		ExternalID:  externalID,
		EventType:   "azure_monitor_" + strings.ToLower(ess.MonitorCondition),
		Severity:    severity,
		Title:       ess.AlertRule,
		Description: ess.Description,
		Host:        host,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
