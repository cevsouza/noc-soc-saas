package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
)

type PostgresSelfHealingRepository struct{}

func NewPostgresSelfHealingRepository() SelfHealingRepository {
	return &PostgresSelfHealingRepository{}
}

func (r *PostgresSelfHealingRepository) CreateAction(ctx context.Context, q db.Queryer, action *model.SelfHealingAction) error {
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}
	action.TenantID = tenantID

	if action.ID == uuid.Nil {
		action.ID = uuid.New()
	}

	query := `
		INSERT INTO self_healing_actions (id, tenant_id, alert_id, script_name, status, execution_output, error_log, attempts, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING created_at, updated_at
	`

	err := q.QueryRow(ctx, query,
		action.ID,
		action.TenantID,
		action.AlertID,
		action.ScriptName,
		action.Status,
		action.ExecutionOutput,
		action.ErrorLog,
		action.Attempts,
	).Scan(&action.CreatedAt, &action.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert self_healing_action: %w", err)
	}

	return nil
}

func (r *PostgresSelfHealingRepository) UpdateAction(ctx context.Context, q db.Queryer, action *model.SelfHealingAction) error {
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	query := `
		UPDATE self_healing_actions
		SET status = $1, execution_output = $2, error_log = $3, attempts = $4, updated_at = NOW()
		WHERE id = $5 AND tenant_id = $6
	`

	cmdTag, err := q.Exec(ctx, query,
		action.Status,
		action.ExecutionOutput,
		action.ErrorLog,
		action.Attempts,
		action.ID,
		tenantID,
	)

	if err != nil {
		return fmt.Errorf("failed to update self_healing_action: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("self healing action not found or not owned by tenant")
	}

	return nil
}

func (r *PostgresSelfHealingRepository) CreateAuditLog(ctx context.Context, q db.Queryer, logEntry *model.AuditLog) error {
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}
	logEntry.TenantID = tenantID

	detailsBytes, err := json.Marshal(logEntry.Details)
	if err != nil {
		return fmt.Errorf("failed to marshal audit details: %w", err)
	}

	if logEntry.ID == uuid.Nil {
		logEntry.ID = uuid.New()
	}

	query := `
		INSERT INTO audit_logs (id, tenant_id, user_id, action, resource, details, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		RETURNING created_at
	`

	err = q.QueryRow(ctx, query,
		logEntry.ID,
		logEntry.TenantID,
		logEntry.UserID,
		logEntry.Action,
		logEntry.Resource,
		detailsBytes,
		logEntry.IPAddress,
	).Scan(&logEntry.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert audit log: %w", err)
	}

	return nil
}
