package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SLA-escalation monitor (Fase 3 refino R2) — a background loop that pages a human when an OPEN
// incident breaches its tenant's SLA. Until now SLA targets were only used to compute the executive
// MTTA/MTTR report after the fact; nothing acted on a breach in real time. This closes that gap.
//
// Design mirrors the connection watchdog (see watchdog.go): a periodic tenant scan, a pure decision
// core, and a per-target Redis flag that suppresses re-paging the same breach more often than the
// re-notify window. Escalation reuses the existing notifier fan-out (triggerEscalations →
// Slack/Teams/e-mail/PagerDuty/Opsgenie), so no new outbound plumbing is introduced.
//
// Scope of v1 (honest): a breach pages the tenant's currently-active escalation channels — the same
// set SOAR escalations use. tenant_sla.escalation_policy_id remains reserved for a richer, named
// escalation-policy engine (per-severity recipient lists, timed escalation levels); that layer is a
// documented future extension, not built here.

const slaEscalationScanInterval = 60 * time.Second

const defaultSLARenotifySecs = 3600 // don't re-page the same breach more than hourly

// slaBreach is the outcome of evaluating one incident against its SLA targets.
type slaBreach int

const (
	breachNone slaBreach = iota
	breachMTTA           // unacknowledged past the MTTA (time-to-acknowledge) target
	breachMTTR           // still unresolved past the MTTR (time-to-resolve) target — the more severe
)

func (b slaBreach) label() string {
	switch b {
	case breachMTTA:
		return "MTTA"
	case breachMTTR:
		return "MTTR"
	default:
		return "none"
	}
}

// evaluateSLABreach is the pure, unit-tested decision core. Given how long an incident has been open
// (minutes since first_seen), whether it has been acknowledged, and whether it's resolved, plus the
// severity's MTTA/MTTR targets, it returns the most severe breach. An unresolved incident past MTTR
// dominates an unacknowledged one past MTTA. Resolved incidents never breach (they shouldn't be
// scanned, but this guards anyway).
func evaluateSLABreach(ageMinutes float64, acknowledged, resolved bool, mttaTarget, mttrTarget float64) slaBreach {
	if resolved {
		return breachNone
	}
	if ageMinutes > mttrTarget {
		return breachMTTR
	}
	if !acknowledged && ageMinutes > mttaTarget {
		return breachMTTA
	}
	return breachNone
}

// StartSLAEscalationMonitor launches the background SLA-breach pager.
func (wp *WorkerPool) StartSLAEscalationMonitor(ctx context.Context) {
	renotifySecs := envInt64("SLA_ESCALATION_RENOTIFY_SECONDS", defaultSLARenotifySecs)
	log.Printf("[SLA] SLA-escalation monitor started (scan=%ds renotify=%ds)", int(slaEscalationScanInterval.Seconds()), renotifySecs)

	ticker := time.NewTicker(slaEscalationScanInterval)
	go func() {
		for {
			select {
			case <-wp.stopChan:
				ticker.Stop()
				return
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				wp.scanSLABreaches(ctx, renotifySecs)
			}
		}
	}()
}

// scanSLABreaches iterates active tenants and evaluates each of their open incidents against the
// tenant's effective SLA targets. Per-tenant work runs inside the tenant RLS context.
func (wp *WorkerPool) scanSLABreaches(ctx context.Context, renotifySecs int64) {
	tenantIDs, err := wp.activeTenantIDs(ctx)
	if err != nil {
		log.Printf("[SLA] Failed to list active tenants: %v", err)
		return
	}
	for _, tenantID := range tenantIDs {
		wp.scanTenantSLABreaches(ctx, tenantID, renotifySecs)
	}
}

type openIncident struct {
	id        uuid.UUID
	severity  string
	status    string
	title     string
	firstSeen time.Time
}

func (wp *WorkerPool) scanTenantSLABreaches(ctx context.Context, tenantID uuid.UUID, renotifySecs int64) {
	tenantCtx := db.WithTenantID(ctx, tenantID)

	var targets map[string]api.SLATarget
	var incidents []openIncident
	err := db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
		t, e := api.LoadEffectiveSLATargets(tenantCtx, tx, tenantID)
		if e != nil {
			return e
		}
		targets = t
		rows, e := tx.Query(tenantCtx, `
			SELECT id, severity, status, title, first_seen
			FROM incidents
			WHERE tenant_id = $1 AND status <> 'resolved'
		`, tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var oi openIncident
			if e := rows.Scan(&oi.id, &oi.severity, &oi.status, &oi.title, &oi.firstSeen); e != nil {
				return e
			}
			incidents = append(incidents, oi)
		}
		return rows.Err()
	})
	if err != nil {
		log.Printf("[SLA] Failed to scan incidents for tenant %s: %v", tenantID, err)
		return
	}

	now := time.Now()
	for _, oi := range incidents {
		target, ok := targets[oi.severity]
		if !ok {
			continue // unknown severity — no target to breach against
		}
		ageMinutes := now.Sub(oi.firstSeen).Minutes()
		breach := evaluateSLABreach(ageMinutes, oi.status == "acknowledged", false, target.MTTATargetMinutes, target.MTTRTargetMinutes)
		if breach == breachNone {
			continue
		}
		wp.escalateSLABreach(ctx, tenantID, oi, breach, ageMinutes, target, renotifySecs)
	}
}

// escalateSLABreach pages the tenant's active escalation channels once per breach, suppressing
// re-pages within the renotify window via a Redis flag keyed by incident + breach type.
func (wp *WorkerPool) escalateSLABreach(ctx context.Context, tenantID uuid.UUID, oi openIncident, breach slaBreach, ageMinutes float64, target api.SLATarget, renotifySecs int64) {
	flagKey := fmt.Sprintf("sla:escalated:%s:%s:%s", tenantID, oi.id, breach.label())
	if _, err := wp.redisClient.Get(ctx, flagKey).Result(); err == nil {
		return // already paged for this breach within the re-notify window
	}

	targetMinutes := target.MTTATargetMinutes
	if breach == breachMTTR {
		targetMinutes = target.MTTRTargetMinutes
	}
	summary := fmt.Sprintf("⏱️ SLA %s estourado: incidente '%s' (%s) aberto há %.0fmin, meta %.0fmin",
		breach.label(), oi.title, oi.severity, ageMinutes, targetMinutes)

	// Synthesize an alert-shaped payload so the existing notifier fan-out can page it unchanged.
	synth := &model.Alert{
		ID:          oi.id,
		TenantID:    tenantID,
		Severity:    model.AlertSeverity(oi.severity),
		Status:      model.AlertTriggered,
		EventType:   "sla_breach_" + breach.label(),
		Summary:     summary,
		Fingerprint: oi.id.String(),
		CreatedAt:   time.Now(),
	}
	wp.triggerEscalations(ctx, synth)
	log.Printf("[SLA] %s breach escalated: incident %s tenant %s (age=%.0fm target=%.0fm)", breach.label(), oi.id, tenantID, ageMinutes, targetMinutes)

	_ = wp.redisClient.Set(ctx, flagKey, time.Now().Unix(), time.Duration(renotifySecs)*time.Second).Err()
}
