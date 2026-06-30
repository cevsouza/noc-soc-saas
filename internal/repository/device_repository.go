package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PostgresDeviceRepository struct{}

func NewPostgresDeviceRepository() DeviceRepository {
	return &PostgresDeviceRepository{}
}

func (r *PostgresDeviceRepository) Create(ctx context.Context, q db.Queryer, device *model.Device) error {
	metadataBytes, err := json.Marshal(device.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal device metadata: %w", err)
	}

	// We retrieve the tenant_id from the context to assign it to the record.
	// This ensures that even if the caller attempts to spoof the tenant, the context is the source of truth.
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}
	device.TenantID = tenantID

	if device.ID == uuid.Nil {
		device.ID = uuid.New()
	}

	query := `
		INSERT INTO devices (id, tenant_id, name, ip_address, type, status, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		RETURNING created_at, updated_at
	`

	err = q.QueryRow(ctx, query,
		device.ID,
		device.TenantID,
		device.Name,
		device.IPAddress,
		device.Type,
		device.Status,
		metadataBytes,
	).Scan(&device.CreatedAt, &device.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert device: %w", err)
	}

	return nil
}

func (r *PostgresDeviceRepository) GetByID(ctx context.Context, q db.Queryer, id uuid.UUID) (*model.Device, error) {
	query := `
		SELECT id, tenant_id, name, ip_address, type, status, metadata, created_at, updated_at
		FROM devices
		WHERE id = $1
	`

	var d model.Device
	var metadataBytes []byte

	err := q.QueryRow(ctx, query, id).Scan(
		&d.ID,
		&d.TenantID,
		&d.Name,
		&d.IPAddress,
		&d.Type,
		&d.Status,
		&metadataBytes,
		&d.CreatedAt,
		&d.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("device not found: %w", err)
		}
		return nil, fmt.Errorf("failed to fetch device: %w", err)
	}

	if err := json.Unmarshal(metadataBytes, &d.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal device metadata: %w", err)
	}

	return &d, nil
}

func (r *PostgresDeviceRepository) List(ctx context.Context, q db.Queryer) ([]*model.Device, error) {
	query := `
		SELECT id, tenant_id, name, ip_address, type, status, metadata, created_at, updated_at
		FROM devices
		ORDER BY name ASC
	`

	rows, err := q.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query devices: %w", err)
	}
	defer rows.Close()

	var devices []*model.Device
	for rows.Next() {
		var d model.Device
		var metadataBytes []byte

		err := rows.Scan(
			&d.ID,
			&d.TenantID,
			&d.Name,
			&d.IPAddress,
			&d.Type,
			&d.Status,
			&metadataBytes,
			&d.CreatedAt,
			&d.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan device: %w", err)
		}

		if err := json.Unmarshal(metadataBytes, &d.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal device metadata: %w", err)
		}

		devices = append(devices, &d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during devices row iteration: %w", err)
	}

	return devices, nil
}

func (r *PostgresDeviceRepository) Update(ctx context.Context, q db.Queryer, device *model.Device) error {
	metadataBytes, err := json.Marshal(device.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal device metadata: %w", err)
	}

	// Double check tenant context matching
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	query := `
		UPDATE devices
		SET name = $1, ip_address = $2, type = $3, status = $4, metadata = $5, updated_at = NOW()
		WHERE id = $6 AND tenant_id = $7
	`

	cmdTag, err := q.Exec(ctx, query,
		device.Name,
		device.IPAddress,
		device.Type,
		device.Status,
		metadataBytes,
		device.ID,
		tenantID,
	)

	if err != nil {
		return fmt.Errorf("failed to update device: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("device not found or not owned by tenant")
	}

	return nil
}

func (r *PostgresDeviceRepository) Delete(ctx context.Context, q db.Queryer, id uuid.UUID) error {
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	query := `
		DELETE FROM devices
		WHERE id = $1 AND tenant_id = $2
	`

	cmdTag, err := q.Exec(ctx, query, id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete device: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("device not found or not owned by tenant")
	}

	return nil
}
