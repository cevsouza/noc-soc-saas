package model

import (
	"time"

	"github.com/google/uuid"
)

type AlertSeverity string

const (
	SeverityInfo     AlertSeverity = "info"
	SeverityWarning  AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
	SeverityFatal    AlertSeverity = "fatal"
)

type AlertStatus string

const (
	AlertTriggered    AlertStatus = "triggered"
	AlertAcknowledged AlertStatus = "acknowledged"
	AlertResolved     AlertStatus = "resolved"
	AlertSuppressed   AlertStatus = "suppressed"
)

type Alert struct {
	ID             uuid.UUID              `json:"id"`
	TenantID       uuid.UUID              `json:"tenant_id"`
	DeviceID       *uuid.UUID             `json:"device_id,omitempty"`
	EventType      string                 `json:"event_type"`
	Severity       AlertSeverity          `json:"severity"`
	Status         AlertStatus            `json:"status"`
	Summary        string                 `json:"summary"`
	Payload        map[string]interface{} `json:"payload"`
	AIAnalysis     map[string]interface{} `json:"ai_analysis,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	ResolvedAt     *time.Time             `json:"resolved_at,omitempty"`
	AcknowledgedAt *time.Time             `json:"acknowledged_at,omitempty"`
	AIDiagnostic   *string                `json:"ai_diagnostic,omitempty"`
	ITSMTicketRef  *string                `json:"itsm_ticket_ref,omitempty"`
	MitreTactics   *string                `json:"mitre_tactics,omitempty"`
	UEBAAnomalous  *bool                  `json:"ueba_anomalous,omitempty"`
	// Fingerprint is a SHA256 content hash (tenant+source+external_id, or a fallback seed when
	// external_id is empty) used for dedupe correlation. Set once at creation, immutable.
	Fingerprint string `json:"fingerprint,omitempty"`
}
