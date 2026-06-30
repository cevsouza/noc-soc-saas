package repository

import (
	"context"
	"errors"
	"fmt"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PostgresVaultRepository struct{}

func NewPostgresVaultRepository() VaultRepository {
	return &PostgresVaultRepository{}
}

func (r *PostgresVaultRepository) CreateSecret(ctx context.Context, q db.Queryer, secret *model.VaultSecret) error {
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}
	secret.TenantID = tenantID

	if secret.ID == uuid.Nil {
		secret.ID = uuid.New()
	}

	query := `
		INSERT INTO tenant_vault (id, tenant_id, secret_key, encrypted_value, nonce, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING created_at, updated_at
	`

	err := q.QueryRow(ctx, query,
		secret.ID,
		secret.TenantID,
		secret.SecretKey,
		secret.EncryptedValue,
		secret.Nonce,
		secret.Description,
	).Scan(&secret.CreatedAt, &secret.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert vault secret: %w", err)
	}

	return nil
}

func (r *PostgresVaultRepository) GetSecretByKey(ctx context.Context, q db.Queryer, secretKey string) (*model.VaultSecret, error) {
	_, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("tenant_id not found in context")
	}

	query := `
		SELECT id, tenant_id, secret_key, encrypted_value, nonce, description, created_at, updated_at
		FROM tenant_vault
		WHERE secret_key = $1
	`

	var secret model.VaultSecret
	err := q.QueryRow(ctx, query, secretKey).Scan(
		&secret.ID,
		&secret.TenantID,
		&secret.SecretKey,
		&secret.EncryptedValue,
		&secret.Nonce,
		&secret.Description,
		&secret.CreatedAt,
		&secret.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("secret not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get vault secret: %w", err)
	}

	return &secret, nil
}

func (r *PostgresVaultRepository) UpdateSecret(ctx context.Context, q db.Queryer, secret *model.VaultSecret) error {
	_, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	query := `
		UPDATE tenant_vault
		SET encrypted_value = $1, nonce = $2, description = $3, updated_at = NOW()
		WHERE secret_key = $4
	`

	cmdTag, err := q.Exec(ctx, query,
		secret.EncryptedValue,
		secret.Nonce,
		secret.Description,
		secret.SecretKey,
	)
	if err != nil {
		return fmt.Errorf("failed to update vault secret: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("secret not found or not owned by tenant")
	}

	return nil
}

func (r *PostgresVaultRepository) DeleteSecret(ctx context.Context, q db.Queryer, secretKey string) error {
	_, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	query := `
		DELETE FROM tenant_vault
		WHERE secret_key = $1
	`

	cmdTag, err := q.Exec(ctx, query, secretKey)
	if err != nil {
		return fmt.Errorf("failed to delete vault secret: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("secret not found or not owned by tenant")
	}

	return nil
}
