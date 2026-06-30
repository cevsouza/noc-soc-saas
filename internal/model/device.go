package model

import (
	"time"

	"github.com/google/uuid"
)

type DeviceStatus string

const (
	DeviceOnline  DeviceStatus = "online"
	DeviceWarning DeviceStatus = "warning"
	DeviceOffline DeviceStatus = "offline"
)

type Device struct {
	ID        uuid.UUID              `json:"id"`
	TenantID  uuid.UUID              `json:"tenant_id"`
	Name      string                 `json:"name"`
	IPAddress string                 `json:"ip_address"`
	Type      string                 `json:"type"` // router, switch, server, firewall
	Status    DeviceStatus           `json:"status"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}
