package worker

import (
	"context"
	"errors"
	"log"

	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// attachIncident groups a freshly-created alert into its incident and links the alert row to it,
// inside a SAVEPOINT so any failure rolls back only this incident work — the alert itself is never
// dropped by a grouping error.
func (wp *WorkerPool) attachIncident(ctx context.Context, tx pgx.Tx, alert *model.Alert, fingerprint string) {
	sp, err := tx.Begin(ctx) // nested tx == SAVEPOINT in pgx
	if err != nil {
		log.Printf("[Incident] Failed to open savepoint for alert %s: %v", alert.ID, err)
		return
	}
	incidentID, gErr := findOrCreateOpenIncident(ctx, sp, alert.TenantID, fingerprint, alert.Summary, string(alert.Severity))
	if gErr == nil {
		_, gErr = sp.Exec(ctx, `UPDATE alerts SET incident_id = $1 WHERE id = $2 AND created_at = $3`, incidentID, alert.ID, alert.CreatedAt)
	}
	if gErr != nil {
		_ = sp.Rollback(ctx)
		log.Printf("[Incident] Grouping failed for alert %s (rolled back savepoint, alert kept): %v", alert.ID, gErr)
		return
	}
	if cErr := sp.Commit(ctx); cErr != nil {
		log.Printf("[Incident] Failed to commit incident grouping for alert %s: %v", alert.ID, cErr)
	}
}

// incidentSeverityRank ranks severities so an incident can track the WORST severity seen across the
// alerts grouped into it. Unknown severities rank lowest.
var incidentSeverityRank = map[string]int{
	"info": 1, "warning": 2, "critical": 3, "fatal": 4,
}

// worseSeverity returns whichever of the two severities is more severe (pure, unit-tested).
func worseSeverity(a, b string) string {
	if incidentSeverityRank[b] > incidentSeverityRank[a] {
		return b
	}
	return a
}

// findOrCreateOpenIncident returns the id of the OPEN incident that groups this fingerprint for the
// tenant, creating one if none exists. On an existing incident it bumps the alert count, refreshes
// last_seen, and raises the severity if this alert is worse. Must run inside the tenant RLS tx.
//
// A partial unique index (idx_incidents_open_fingerprint) guarantees at most one open incident per
// (tenant, fingerprint); if two workers race to create one, the loser catches the unique-violation
// and falls back to bumping the winner's incident.
func findOrCreateOpenIncident(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, fingerprint, title, severity string) (uuid.UUID, error) {
	bumpExisting := func() (uuid.UUID, bool, error) {
		var id uuid.UUID
		var cur string
		err := tx.QueryRow(ctx, `
			SELECT id, severity FROM incidents
			WHERE tenant_id = $1 AND fingerprint = $2 AND status <> 'resolved'
			LIMIT 1
		`, tenantID, fingerprint).Scan(&id, &cur)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		if err != nil {
			return uuid.Nil, false, err
		}
		_, err = tx.Exec(ctx, `
			UPDATE incidents
			SET alert_count = alert_count + 1, last_seen = NOW(), updated_at = NOW(), severity = $2
			WHERE id = $1
		`, id, worseSeverity(cur, severity))
		return id, true, err
	}

	if id, found, err := bumpExisting(); err != nil || found {
		return id, err
	}

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO incidents (tenant_id, fingerprint, title, severity, status, alert_count)
		VALUES ($1, $2, $3, $4, 'open', 1)
		RETURNING id
	`, tenantID, fingerprint, title, severity).Scan(&id)
	if err != nil {
		// Lost a race to another worker creating the same open incident — bump theirs instead.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if existingID, found, berr := bumpExisting(); berr == nil && found {
				return existingID, nil
			}
		}
		return uuid.Nil, err
	}
	return id, nil
}
