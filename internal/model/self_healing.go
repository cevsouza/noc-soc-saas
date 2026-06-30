package model

import (
	"time"

	"github.com/google/uuid"
)

type SelfHealingStatus string

const (
	HealingPending SelfHealingStatus = "pending"
	HealingRunning SelfHealingStatus = "running"
	HealingSuccess SelfHealingStatus = "success"
	HealingFailed  SelfHealingStatus = "failed"
)

type SelfHealingAction struct {
	ID              uuid.UUID         `json:"id"`
	TenantID        uuid.UUID         `json:"tenant_id"`
	AlertID         uuid.UUID         `json:"alert_id"`
	ScriptName      string            `json:"script_name"`
	Status          SelfHealingStatus `json:"status"`
	ExecutionOutput *string           `json:"execution_output,omitempty"`
	ErrorLog        *string           `json:"error_log,omitempty"`
	Attempts        int               `json:"attempts"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}
