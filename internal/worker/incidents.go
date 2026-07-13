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

// riskSeverityBase is the severity contribution to an incident's dynamic risk score.
var riskSeverityBase = map[string]int{"info": 10, "warning": 30, "critical": 50, "fatal": 70}

// computeRiskScore derives a dynamic 0-100 risk score (Fase 3/3c) combining the incident's worst
// severity with its recurrence (alert_count — how persistent the problem is / tenant history). This
// is deliberately more than static severity: a recurring critical outranks a one-off critical.
// Pure and unit-tested. (Asset criticality and threat-intel confidence are documented future
// inputs — see migration 000020 — not yet available as data.)
func computeRiskScore(severity string, recurrence int) int {
	score := riskSeverityBase[severity]
	if recurrence < 1 {
		recurrence = 1
	}
	bonus := recurrence * 3
	if bonus > 30 {
		bonus = 30
	}
	score += bonus
	if score > 100 {
		score = 100
	}
	return score
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
		var count int
		err := tx.QueryRow(ctx, `
			SELECT id, severity, alert_count FROM incidents
			WHERE tenant_id = $1 AND fingerprint = $2 AND status <> 'resolved'
			LIMIT 1
		`, tenantID, fingerprint).Scan(&id, &cur, &count)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		if err != nil {
			return uuid.Nil, false, err
		}
		worst := worseSeverity(cur, severity)
		newCount := count + 1
		_, err = tx.Exec(ctx, `
			UPDATE incidents
			SET alert_count = alert_count + 1, last_seen = NOW(), updated_at = NOW(), severity = $2, risk_score = $3
			WHERE id = $1
		`, id, worst, computeRiskScore(worst, newCount))
		return id, true, err
	}

	if id, found, err := bumpExisting(); err != nil || found {
		return id, err
	}

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO incidents (tenant_id, fingerprint, title, severity, status, alert_count, risk_score)
		VALUES ($1, $2, $3, $4, 'open', 1, $5)
		RETURNING id
	`, tenantID, fingerprint, title, severity, computeRiskScore(severity, 1)).Scan(&id)
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
