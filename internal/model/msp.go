package model

import (
	"time"

	"github.com/google/uuid"
)

type MSPOrganization struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"created_at"`
}

type TenantMappingRule struct {
	ID              uuid.UUID `json:"id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	SourceTool      string    `json:"source_tool"`
	SourceField     string    `json:"source_field"`
	SourceValue     string    `json:"source_value"`
	NormalizedValue string    `json:"normalized_value"`
	CreatedAt       time.Time `json:"created_at"`
}

type ShiftHandover struct {
	ID                 uuid.UUID  `json:"id"`
	MSPID              uuid.UUID  `json:"msp_id"`
	OutgoingOperatorID uuid.UUID  `json:"outgoing_operator_id"`
	IncomingOperatorID *uuid.UUID `json:"incoming_operator_id,omitempty"`
	ShiftSummary       string     `json:"shift_summary"`
	PendingAlertsCount int        `json:"pending_alerts_count"`
	Status             string     `json:"status"` // "pending", "acknowledged"
	AcknowledgedAt     *time.Time `json:"acknowledged_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`

	// UI metadata populated by join
	OutgoingOperatorName string `json:"outgoing_operator_name,omitempty"`
	IncomingOperatorName string `json:"incoming_operator_name,omitempty"`
}
