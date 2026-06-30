package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PostgresAlertRepository struct{}

func NewPostgresAlertRepository() AlertRepository {
	return &PostgresAlertRepository{}
}

func (r *PostgresAlertRepository) Create(ctx context.Context, q db.Queryer, alert *model.Alert) error {
	payloadBytes, err := json.Marshal(alert.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal alert payload: %w", err)
	}

	var aiAnalysisBytes []byte
	if alert.AIAnalysis != nil {
		aiAnalysisBytes, err = json.Marshal(alert.AIAnalysis)
		if err != nil {
			return fmt.Errorf("failed to marshal alert AI analysis: %w", err)
		}
	}

	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}
	alert.TenantID = tenantID

	if alert.ID == uuid.Nil {
		alert.ID = uuid.New()
	}
	if alert.CreatedAt.IsZero() {
		alert.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO alerts (id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		RETURNING updated_at
	`

	err = q.QueryRow(ctx, query,
		alert.ID,
		alert.TenantID,
		alert.DeviceID,
		alert.EventType,
		alert.Severity,
		alert.Status,
		alert.Summary,
		payloadBytes,
		aiAnalysisBytes,
		alert.CreatedAt,
	).Scan(&alert.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert alert: %w", err)
	}

	return nil
}

// GetByID uses BOTH id and createdAt to allow PostgreSQL "partition pruning"
// (searching only the targeted partition instead of doing an expensive global scan).
func (r *PostgresAlertRepository) GetByID(ctx context.Context, q db.Queryer, id uuid.UUID, createdAt time.Time) (*model.Alert, error) {
	query := `
		SELECT id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at, resolved_at
		FROM alerts
		WHERE id = $1 AND created_at = $2
	`

	var a model.Alert
	var payloadBytes []byte
	var aiAnalysisBytes []byte

	err := q.QueryRow(ctx, query, id, createdAt).Scan(
		&a.ID,
		&a.TenantID,
		&a.DeviceID,
		&a.EventType,
		&a.Severity,
		&a.Status,
		&a.Summary,
		&payloadBytes,
		&aiAnalysisBytes,
		&a.CreatedAt,
		&a.UpdatedAt,
		&a.ResolvedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("alert not found: %w", err)
		}
		return nil, fmt.Errorf("failed to fetch alert: %w", err)
	}

	if err := json.Unmarshal(payloadBytes, &a.Payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal alert payload: %w", err)
	}

	if len(aiAnalysisBytes) > 0 {
		if err := json.Unmarshal(aiAnalysisBytes, &a.AIAnalysis); err != nil {
			return nil, fmt.Errorf("failed to unmarshal alert AI analysis: %w", err)
		}
	}

	return &a, nil
}

func (r *PostgresAlertRepository) List(ctx context.Context, q db.Queryer, limit, offset int) ([]*model.Alert, error) {
	query := `
		SELECT id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at, resolved_at
		FROM alerts
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := q.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*model.Alert
	for rows.Next() {
		var a model.Alert
		var payloadBytes []byte
		var aiAnalysisBytes []byte

		err := rows.Scan(
			&a.ID,
			&a.TenantID,
			&a.DeviceID,
			&a.EventType,
			&a.Severity,
			&a.Status,
			&a.Summary,
			&payloadBytes,
			&aiAnalysisBytes,
			&a.CreatedAt,
			&a.UpdatedAt,
			&a.ResolvedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}

		if err := json.Unmarshal(payloadBytes, &a.Payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal alert payload: %w", err)
		}

		if len(aiAnalysisBytes) > 0 {
			if err := json.Unmarshal(aiAnalysisBytes, &a.AIAnalysis); err != nil {
				return nil, fmt.Errorf("failed to unmarshal alert AI analysis: %w", err)
			}
		}

		alerts = append(alerts, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during alerts row iteration: %w", err)
	}

	return alerts, nil
}

func (r *PostgresAlertRepository) UpdateStatus(ctx context.Context, q db.Queryer, id uuid.UUID, createdAt time.Time, status model.AlertStatus) error {
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	var query string
	var err error
	var cmdTag pgconn.CommandTag

	if status == model.AlertResolved {
		query = `
			UPDATE alerts
			SET status = $1, resolved_at = NOW(), updated_at = NOW()
			WHERE id = $2 AND created_at = $3 AND tenant_id = $4
		`
		cmdTag, err = q.Exec(ctx, query, status, id, createdAt, tenantID)
	} else {
		query = `
			UPDATE alerts
			SET status = $1, resolved_at = NULL, updated_at = NOW()
			WHERE id = $2 AND created_at = $3 AND tenant_id = $4
		`
		cmdTag, err = q.Exec(ctx, query, status, id, createdAt, tenantID)
	}

	if err != nil {
		return fmt.Errorf("failed to update alert status: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("alert not found or not owned by tenant")
	}

	return nil
}

func (r *PostgresAlertRepository) Update(ctx context.Context, q db.Queryer, alert *model.Alert) error {
	tenantID, ok := db.TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	payloadBytes, err := json.Marshal(alert.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal alert payload: %w", err)
	}

	var aiAnalysisBytes []byte
	if alert.AIAnalysis != nil {
		aiAnalysisBytes, err = json.Marshal(alert.AIAnalysis)
		if err != nil {
			return fmt.Errorf("failed to marshal alert AI analysis: %w", err)
		}
	}

	query := `
		UPDATE alerts
		SET status = $1, summary = $2, payload = $3, ai_analysis = $4, updated_at = NOW(), resolved_at = $5
		WHERE id = $6 AND created_at = $7 AND tenant_id = $8
	`

	cmdTag, err := q.Exec(ctx, query,
		alert.Status,
		alert.Summary,
		payloadBytes,
		aiAnalysisBytes,
		alert.ResolvedAt,
		alert.ID,
		alert.CreatedAt,
		tenantID,
	)

	if err != nil {
		return fmt.Errorf("failed to update alert: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return fmt.Errorf("alert not found or not owned by tenant")
	}

	return nil
}

