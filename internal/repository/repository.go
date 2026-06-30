package repository

import (
	"context"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
)

// DeviceRepository defines the data access contract for network devices.
type DeviceRepository interface {
	Create(ctx context.Context, q db.Queryer, device *model.Device) error
	GetByID(ctx context.Context, q db.Queryer, id uuid.UUID) (*model.Device, error)
	List(ctx context.Context, q db.Queryer) ([]*model.Device, error)
	Update(ctx context.Context, q db.Queryer, device *model.Device) error
	Delete(ctx context.Context, q db.Queryer, id uuid.UUID) error
}

// AlertRepository defines the data access contract for system/network alerts.
type AlertRepository interface {
	Create(ctx context.Context, q db.Queryer, alert *model.Alert) error
	GetByID(ctx context.Context, q db.Queryer, id uuid.UUID, createdAt time.Time) (*model.Alert, error)
	List(ctx context.Context, q db.Queryer, limit, offset int) ([]*model.Alert, error)
	UpdateStatus(ctx context.Context, q db.Queryer, id uuid.UUID, createdAt time.Time, status model.AlertStatus) error
	Update(ctx context.Context, q db.Queryer, alert *model.Alert) error
}

// SelfHealingRepository handles automation and system auditing execution logs.
type SelfHealingRepository interface {
	CreateAction(ctx context.Context, q db.Queryer, action *model.SelfHealingAction) error
	UpdateAction(ctx context.Context, q db.Queryer, action *model.SelfHealingAction) error
	CreateAuditLog(ctx context.Context, q db.Queryer, log *model.AuditLog) error
}

// VaultRepository defines the data access contract for securely stored credentials.
type VaultRepository interface {
	CreateSecret(ctx context.Context, q db.Queryer, secret *model.VaultSecret) error
	GetSecretByKey(ctx context.Context, q db.Queryer, secretKey string) (*model.VaultSecret, error)
	UpdateSecret(ctx context.Context, q db.Queryer, secret *model.VaultSecret) error
	DeleteSecret(ctx context.Context, q db.Queryer, secretKey string) error
}


