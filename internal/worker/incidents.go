package worker

import (
	"context"
	"errors"
	"log"
	"strings"

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
	// Resolve the alerting host to a managed asset's business criticality (topology slice T2), so an
	// incident that hits a critical asset scores higher than the same alert on an ordinary host.
	host, _ := alert.AIAnalysis["host"].(string)
	criticality := lookupAssetCriticality(ctx, sp, alert.TenantID, host)
	incidentID, gErr := findOrCreateOpenIncident(ctx, sp, alert.TenantID, fingerprint, alert.Summary, string(alert.Severity), criticality)
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

// riskCriticalityBonus adds to the risk score when the incident hits a MANAGED asset of above-normal
// business criticality (topology slice T2's assets.business_criticality). Low/medium are the neutral
// default and add nothing, so an incident on an unmanaged or ordinary host scores exactly as it did
// before this input existed (no silent regression for existing incidents). Backlog B1 / MSSP R3.
var riskCriticalityBonus = map[string]int{"low": 0, "medium": 0, "high": 15, "critical": 25}

// computeRiskScore derives a dynamic 0-100 risk score (Fase 3/3c) combining the incident's worst
// severity, its recurrence (alert_count — how persistent the problem is / tenant history), and the
// business criticality of the affected asset (B1). This is deliberately more than static severity:
// a recurring critical on a critical asset outranks a one-off critical on an ordinary host. Pure and
// unit-tested. (Threat-intel confidence remains a documented future input — needs a real IOC feed.)
func computeRiskScore(severity string, recurrence int, criticality string) int {
	score := riskSeverityBase[severity]
	if recurrence < 1 {
		recurrence = 1
	}
	bonus := recurrence * 3
	if bonus > 30 {
		bonus = 30
	}
	score += bonus
	score += riskCriticalityBonus[criticality]
	if score > 100 {
		score = 100
	}
	return score
}

// lookupAssetCriticality resolves an alerting host to a managed asset's business criticality (via the
// asset identifier or one of its aliases, case-insensitively — mirroring the topology host matching)
// so it can raise the incident risk score. Returns "" when the host isn't a managed asset (no bonus).
// Must run inside the tenant RLS tx; a transient query error degrades to "" rather than failing the
// grouping (the SAVEPOINT caller keeps the alert regardless).
func lookupAssetCriticality(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	var crit string
	err := tx.QueryRow(ctx, `
		SELECT business_criticality FROM assets
		WHERE tenant_id = $1
		  AND (LOWER(identifier) = LOWER($2)
		       OR EXISTS (SELECT 1 FROM unnest(aliases) a WHERE LOWER(a) = LOWER($2)))
		LIMIT 1
	`, tenantID, host).Scan(&crit)
	if err != nil {
		return ""
	}
	return crit
}

// findOrCreateOpenIncident returns the id of the OPEN incident that groups this fingerprint for the
// tenant, creating one if none exists. On an existing incident it bumps the alert count, refreshes
// last_seen, and raises the severity if this alert is worse. Must run inside the tenant RLS tx.
//
// A partial unique index (idx_incidents_open_fingerprint) guarantees at most one open incident per
// (tenant, fingerprint); if two workers race to create one, the loser catches the unique-violation
// and falls back to bumping the winner's incident.
func findOrCreateOpenIncident(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, fingerprint, title, severity, criticality string) (uuid.UUID, error) {
	bumpExisting := func() (uuid.UUID, bool, error) {
		var id uuid.UUID
		var cur, curCrit string
		var count int
		err := tx.QueryRow(ctx, `
			SELECT id, severity, alert_count, COALESCE(asset_criticality, '') FROM incidents
			WHERE tenant_id = $1 AND fingerprint = $2 AND status <> 'resolved'
			LIMIT 1
		`, tenantID, fingerprint).Scan(&id, &cur, &count, &curCrit)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		if err != nil {
			return uuid.Nil, false, err
		}
		worst := worseSeverity(cur, severity)
		// Keep the incident's known criticality when this particular alert can't resolve one (a later
		// alert on the same problem may have arrived without a host), so a critical asset isn't downgraded.
		effCrit := criticality
		if effCrit == "" {
			effCrit = curCrit
		}
		newCount := count + 1
		_, err = tx.Exec(ctx, `
			UPDATE incidents
			SET alert_count = alert_count + 1, last_seen = NOW(), updated_at = NOW(), severity = $2, risk_score = $3, asset_criticality = NULLIF($4, '')
			WHERE id = $1
		`, id, worst, computeRiskScore(worst, newCount, effCrit), effCrit)
		return id, true, err
	}

	if id, found, err := bumpExisting(); err != nil || found {
		return id, err
	}

	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO incidents (tenant_id, fingerprint, title, severity, status, alert_count, risk_score, asset_criticality)
		VALUES ($1, $2, $3, $4, 'open', 1, $5, NULLIF($6, ''))
		RETURNING id
	`, tenantID, fingerprint, title, severity, computeRiskScore(severity, 1, criticality), criticality).Scan(&id)
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
