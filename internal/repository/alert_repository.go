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
	"github.com/jackc/pgx/v5/pgconn"
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
		INSERT INTO alerts (id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at, acknowledged_at, ai_diagnostic, itsm_ticket_ref, mitre_tactics, ueba_anomalous, fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), $11, $12, $13, $14, $15, $16)
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
		alert.AcknowledgedAt,
		alert.AIDiagnostic,
		alert.ITSMTicketRef,
		alert.MitreTactics,
		alert.UEBAAnomalous,
		alert.Fingerprint,
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
		SELECT id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at, resolved_at, acknowledged_at, ai_diagnostic, itsm_ticket_ref, mitre_tactics, ueba_anomalous, COALESCE(fingerprint, ''), incident_id
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
		&a.AcknowledgedAt,
		&a.AIDiagnostic,
		&a.ITSMTicketRef,
		&a.MitreTactics,
		&a.UEBAAnomalous,
		&a.Fingerprint,
		&a.IncidentID,
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

// List returns alerts filtered explicitly by tenant_id. This filter is not merely an
// optimization: it is a defense-in-depth guarantee that this query cannot return
// cross-tenant data even if the caller forgets to run it inside a tenant-scoped
// transaction (i.e. even if RLS is not enforced for whatever reason).
const alertSelectCols = `id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at, resolved_at, acknowledged_at, ai_diagnostic, itsm_ticket_ref, mitre_tactics, ueba_anomalous, COALESCE(fingerprint, ''), incident_id`

// scanAlertRows scans a result set selecting alertSelectCols into a slice of alerts.
func scanAlertRows(rows pgx.Rows) ([]*model.Alert, error) {
	var alerts []*model.Alert
	for rows.Next() {
		var a model.Alert
		var payloadBytes []byte
		var aiAnalysisBytes []byte
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.DeviceID, &a.EventType, &a.Severity, &a.Status, &a.Summary,
			&payloadBytes, &aiAnalysisBytes, &a.CreatedAt, &a.UpdatedAt, &a.ResolvedAt, &a.AcknowledgedAt,
			&a.AIDiagnostic, &a.ITSMTicketRef, &a.MitreTactics, &a.UEBAAnomalous, &a.Fingerprint, &a.IncidentID,
		); err != nil {
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

func (r *PostgresAlertRepository) List(ctx context.Context, q db.Queryer, tenantID uuid.UUID, limit, offset int) ([]*model.Alert, error) {
	rows, err := q.Query(ctx, `SELECT `+alertSelectCols+` FROM alerts WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}
	defer rows.Close()
	return scanAlertRows(rows)
}

// ListByDomain is List filtered to alerts whose stored source (ai_analysis->>'source') is in the
// given set — powering the segregated NOC vs SOC consoles.
func (r *PostgresAlertRepository) ListByDomain(ctx context.Context, q db.Queryer, tenantID uuid.UUID, sources []string, limit, offset int) ([]*model.Alert, error) {
	rows, err := q.Query(ctx, `SELECT `+alertSelectCols+` FROM alerts WHERE tenant_id = $1 AND ai_analysis->>'source' = ANY($2) ORDER BY created_at DESC LIMIT $3 OFFSET $4`, tenantID, sources, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts by domain: %w", err)
	}
	defer rows.Close()
	return scanAlertRows(rows)
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
		SET status = $1, summary = $2, payload = $3, ai_analysis = $4, updated_at = NOW(), resolved_at = $5, acknowledged_at = $6, ai_diagnostic = $7, itsm_ticket_ref = $8, mitre_tactics = $9, ueba_anomalous = $10
		WHERE id = $11 AND created_at = $12 AND tenant_id = $13
	`

	cmdTag, err := q.Exec(ctx, query,
		alert.Status,
		alert.Summary,
		payloadBytes,
		aiAnalysisBytes,
		alert.ResolvedAt,
		alert.AcknowledgedAt,
		alert.AIDiagnostic,
		alert.ITSMTicketRef,
		alert.MitreTactics,
		alert.UEBAAnomalous,
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

