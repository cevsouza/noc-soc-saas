package model

import (
	"time"

	"github.com/google/uuid"
)

// VaultSecret represents an encrypted credential or API token stored securely.
type VaultSecret struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	SecretKey      string    `json:"secret_key"`
	EncryptedValue []byte    `json:"-"`
	Nonce          []byte    `json:"-"`
	Description    string    `json:"description,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
