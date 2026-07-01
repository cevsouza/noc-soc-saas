package model

import (
	"time"

	"github.com/google/uuid"
)

type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

type Tenant struct {
	ID        uuid.UUID    `json:"id"`
	Name      string       `json:"name"`
	Slug      string       `json:"slug"`
	Status    TenantStatus `json:"status"`
	LogoURL   *string      `json:"logo_url,omitempty"`
	PrimaryColor *string   `json:"primary_color,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}
