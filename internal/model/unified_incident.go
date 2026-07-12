package model

import (
	"time"

	"github.com/google/uuid"
)

type IncidentSource string

const (
	SourcePrometheus   IncidentSource = "prometheus"
	SourceWazuh        IncidentSource = "wazuh"
	SourceSentinel     IncidentSource = "sentinel"
	SourceUptimeKuma   IncidentSource = "uptimekuma"
	SourceGrafana      IncidentSource = "grafana"
	SourceZabbix       IncidentSource = "zabbix"
	SourceSystem       IncidentSource = "system"
	SourceOTLP         IncidentSource = "otlp"
	SourceIcinga       IncidentSource = "icinga"
	SourceCloudWatch   IncidentSource = "cloudwatch"
	SourceAzureMonitor IncidentSource = "azuremonitor"
	SourcePagerDuty    IncidentSource = "pagerduty"
	SourceOpsgenie     IncidentSource = "opsgenie"
	SourceCrowdStrike  IncidentSource = "crowdstrike"
	SourcePaloAlto     IncidentSource = "paloalto"
	SourceFortinet     IncidentSource = "fortinet"
)

// UnifiedIncident is the normalized internal JSON structure used by the SaaS NOC/SOC engine.
// All alerts ingested from external providers are mapped to this format before queuing.
type UnifiedIncident struct {
	ID          uuid.UUID              `json:"id"`
	TenantID    uuid.UUID              `json:"tenant_id"`
	DeviceID    *uuid.UUID             `json:"device_id,omitempty"`
	Source      IncidentSource         `json:"source"`
	ExternalID  string                 `json:"external_id"`
	EventType   string                 `json:"event_type"`
	Severity    AlertSeverity          `json:"severity"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Host        string                 `json:"host"`
	RawPayload  map[string]interface{} `json:"raw_payload"`
	Timestamp   time.Time              `json:"timestamp"`
}
