package repository

import (
	"context"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
)

// AlertHistoryFilter carries the optional filters for the History/search view. A zero value means
// "no filter" for each field: empty strings and a nil Sources match everything; SinceHours <= 0
// applies no lower time bound.
type AlertHistoryFilter struct {
	Severity   string   // exact severity match (info|warning|critical|fatal)
	Status     string   // exact status match (triggered|acknowledged|resolved|suppressed)
	Search     string   // ILIKE substring over summary and event_type
	Sources    []string // NOC/SOC domain sources; nil = all domains
	SinceHours int      // only alerts created within the last N hours; <= 0 = no bound
}

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
	List(ctx context.Context, q db.Queryer, tenantID uuid.UUID, limit, offset int) ([]*model.Alert, error)
	// ListByDomain is List filtered to alerts whose source is in the given set (NOC vs SOC console).
	ListByDomain(ctx context.Context, q db.Queryer, tenantID uuid.UUID, sources []string, limit, offset int) ([]*model.Alert, error)
	// ListOpen returns only unresolved/unsuppressed alerts, ordered by urgency (severity rank then
	// recency), so the operational console never drops an old-but-still-open alert off the recency
	// window. A nil/empty sources slice means all domains; a non-empty one scopes to a NOC/SOC domain.
	ListOpen(ctx context.Context, q db.Queryer, tenantID uuid.UUID, sources []string, limit, offset int) ([]*model.Alert, error)
	// ListHistory returns alerts of ANY status matching the given filters (all empty = everything),
	// newest first, paginated — powering the History/search view where resolved alerts can be found
	// and reopened. All filters are optional; the tenant_id filter is always applied.
	ListHistory(ctx context.Context, q db.Queryer, tenantID uuid.UUID, f AlertHistoryFilter, limit, offset int) ([]*model.Alert, error)
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


