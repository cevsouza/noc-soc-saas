package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"noc-api/internal/cache"
	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Operational KPI window and heuristics. The window matches the SLA executive report (30 days)
// so both panels describe the same period. estimatedMinutesSavedPerAutomation is a deliberately
// conservative, documented heuristic — each hands-off automated remediation (a successful SOAR
// runbook or an executed containment action) is credited with 15 minutes of analyst time that
// would otherwise have been spent doing it by hand. Adjust in one place if the business wants a
// different assumption.
const (
	operationalWindowDays              = 30
	silentSourceThresholdSeconds       = 3600 // a source with no heartbeat in the last hour is "silent"
	estimatedMinutesSavedPerAutomation = 15.0
)

// Marker prefixes for the incident_comments this package's KPIs count. Defined here (the api package)
// as the single source of truth: the reopen handler (same package) and the SLA-escalation worker
// (which imports api) build their comments with these prefixes, and the operational stats query
// counts comments matching them. Keep the written text starting with the prefix if it ever changes.
const (
	// AlertReopenedCommentPrefix marks the comment written when a closed alert is reopened.
	AlertReopenedCommentPrefix = "Alerta REABERTO"
	// SLAEscalatedCommentPrefix marks the comment the SLA-escalation worker writes on a breach page.
	SLAEscalatedCommentPrefix = "[SLA] Incidente escalado"
)

// OperationalStats is the daily/weekly tactical KPI bundle that complements the SLA executive
// report — the metrics a NOC/SOC watches to spot alert fatigue, offending assets, automation
// ROI, MITRE coverage, and silent telemetry sources.
type OperationalStats struct {
	WindowDays    int               `json:"window_days"`
	TriageBacklog TriageBacklog     `json:"triage_backlog"`
	NoiseRatio    NoiseRatio        `json:"noise_ratio"`
	TopOffenders  []OffenderCount   `json:"top_offenders"`
	Automation    AutomationStats   `json:"automation"`
	Rework        ReworkStats       `json:"rework"`
	Escalations   EscalationStats   `json:"escalations"`
	Disposition   DispositionStats  `json:"disposition"`
	Detection     DetectionStats    `json:"detection"`
	ByMitre       []MitreCount      `json:"by_mitre"`
	SourceHealth  []SourceHeartbeat `json:"source_health"`
}

// DetectionStats is the MTTD (mean time to detect) breakdown (K4): the delay between an event
// happening at the source (alerts.created_at = the connector-parsed origin time) and the platform
// ingesting it (alerts.ingested_at). Only alerts with a usable origin timestamp are counted — a
// detection delay between 1s and 7d — so alerts whose source carries no origin time (delay ~0) and
// clock-skew/backfill garbage (delay > 7d) are excluded from the average. InstrumentedPct shows how
// much of the alert volume actually carries a source timestamp.
type DetectionStats struct {
	TotalAlerts     int     `json:"total_alerts"`
	Instrumented    int     `json:"instrumented"`
	InstrumentedPct float64 `json:"instrumented_pct"`
	AvgMTTDSeconds  float64 `json:"avg_mttd_seconds"`
}

// DispositionStats is the detection-quality breakdown (K5): of the incidents an analyst classified in
// the window, how many were real (true positive), false alarms (false positive), or expected/benign,
// and the resulting false-positive rate. A high FP rate means detection rules need tuning.
type DispositionStats struct {
	TruePositive         int     `json:"true_positive"`
	FalsePositive        int     `json:"false_positive"`
	Benign               int     `json:"benign"`
	Classified           int     `json:"classified"`
	FalsePositiveRatePct float64 `json:"false_positive_rate_pct"`
}

// ReworkStats measures closure quality (K1): how often a resolved/suppressed alert had to be reopened
// (bounced back into the working queue) within the window. A high reopen rate signals alerts being
// closed prematurely. Closed = resolved-in-window + reopened (everything that got closed).
type ReworkStats struct {
	Reopened      int     `json:"reopened"`
	Closed        int     `json:"closed"`
	ReopenRatePct float64 `json:"reopen_rate_pct"`
}

// EscalationStats counts SLA-breach escalations paged in the window — incidents that blew past their
// MTTA/MTTR target and paged a human (each page is stamped on the incident timeline by the worker).
type EscalationStats struct {
	SLABreaches int `json:"sla_breaches"`
}

// TriageBacklog is the current (not windowed) count of unresolved alerts awaiting attention.
type TriageBacklog struct {
	Triggered    int `json:"triggered"`
	Acknowledged int `json:"acknowledged"`
}

// NoiseRatio measures alert fatigue: total raw alerts vs the number of distinct incidents they
// collapse into (by fingerprint). A high ratio means thresholds need tuning.
type NoiseRatio struct {
	TotalAlerts       int     `json:"total_alerts"`
	DistinctIncidents int     `json:"distinct_incidents"`
	Ratio             float64 `json:"ratio"`
}

// OffenderCount is one "top offender" — an event type responsible for a large share of alerts.
type OffenderCount struct {
	EventType string `json:"event_type"`
	Count     int    `json:"count"`
}

// AutomationStats quantifies SOAR/response automation ROI over the window.
type AutomationStats struct {
	SoarExecuted        int     `json:"soar_executed"`
	SoarFailed          int     `json:"soar_failed"`
	ResponseExecuted    int     `json:"response_executed"`
	ResponseFailed      int     `json:"response_failed"`
	EstimatedHoursSaved float64 `json:"estimated_hours_saved"`
}

// MitreCount is the alert volume attributed to a MITRE ATT&CK tactic string.
type MitreCount struct {
	Tactic string `json:"tactic"`
	Count  int    `json:"count"`
}

// SourceHeartbeat reports whether a tenant's active integration is still sending telemetry.
// LastSeenSecondsAgo is -1 when the source has never reported a heartbeat.
type SourceHeartbeat struct {
	Type               string `json:"type"`
	LastSeenSecondsAgo int64  `json:"last_seen_seconds_ago"`
	Silent             bool   `json:"silent"`
}

// computeNoiseRatio returns total/distinct, guarding division by zero (0 distinct → ratio 0).
func computeNoiseRatio(total, distinct int) float64 {
	if distinct <= 0 {
		return 0
	}
	return float64(total) / float64(distinct)
}

// estimateHoursSaved credits each successful hands-off automation with a fixed minutes-saved
// heuristic and returns the total in hours.
func estimateHoursSaved(soarExecuted, responseExecuted int) float64 {
	return float64(soarExecuted+responseExecuted) * estimatedMinutesSavedPerAutomation / 60.0
}

// reopenRatePct returns reopened/closed as a percentage, guarding division by zero (0 closed → 0%).
func reopenRatePct(reopened, closed int) float64 {
	if closed <= 0 {
		return 0
	}
	return float64(reopened) / float64(closed) * 100
}

// falsePositiveRatePct returns falsePositive/classified as a percentage, guarding division by zero
// (0 classified → 0%). Denominator is all classified incidents (TP + FP + benign).
func falsePositiveRatePct(falsePositive, classified int) float64 {
	if classified <= 0 {
		return 0
	}
	return float64(falsePositive) / float64(classified) * 100
}

// detectionCoveragePct returns instrumented/total as a percentage (share of alerts carrying a usable
// source timestamp), guarding division by zero.
func detectionCoveragePct(instrumented, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(instrumented) / float64(total) * 100
}

// HandleGetOperationalStats returns the tactical KPI bundle for the caller's tenant. Any
// authenticated user may read it (same access level as the SLA stats endpoint); all Postgres
// reads run inside the tenant-scoped RLS transaction, and the Redis heartbeat lookups are keyed
// by the same tenant id.
func HandleGetOperationalStats(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		stats := OperationalStats{
			WindowDays:   operationalWindowDays,
			TopOffenders: []OffenderCount{},
			ByMitre:      []MitreCount{},
			SourceHealth: []SourceHeartbeat{},
		}
		var activeTypes []string

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			window := fmt.Sprintf("%d days", operationalWindowDays)

			// 1. Triage backlog (current open counts, not windowed).
			if err := tx.QueryRow(ctx, `
				SELECT
					COUNT(*) FILTER (WHERE status = 'triggered'),
					COUNT(*) FILTER (WHERE status = 'acknowledged')
				FROM alerts WHERE tenant_id = $1
			`, tenantID).Scan(&stats.TriageBacklog.Triggered, &stats.TriageBacklog.Acknowledged); err != nil {
				return err
			}

			// 2. Noise ratio (windowed): total alerts vs distinct fingerprints.
			if err := tx.QueryRow(ctx, `
				SELECT
					COUNT(*),
					COUNT(DISTINCT COALESCE(NULLIF(fingerprint, ''), id::text))
				FROM alerts
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval
			`, tenantID, window).Scan(&stats.NoiseRatio.TotalAlerts, &stats.NoiseRatio.DistinctIncidents); err != nil {
				return err
			}
			stats.NoiseRatio.Ratio = computeNoiseRatio(stats.NoiseRatio.TotalAlerts, stats.NoiseRatio.DistinctIncidents)

			// 3. Top offenders by event type (windowed).
			offRows, err := tx.Query(ctx, `
				SELECT event_type, COUNT(*) AS c
				FROM alerts
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval
				GROUP BY event_type
				ORDER BY c DESC
				LIMIT 5
			`, tenantID, window)
			if err != nil {
				return err
			}
			for offRows.Next() {
				var o OffenderCount
				if err := offRows.Scan(&o.EventType, &o.Count); err != nil {
					offRows.Close()
					return err
				}
				stats.TopOffenders = append(stats.TopOffenders, o)
			}
			offRows.Close()
			if err := offRows.Err(); err != nil {
				return err
			}

			// 4a. SOAR automation (runbook execution logs; status is 'sucesso'/'falha').
			if err := tx.QueryRow(ctx, `
				SELECT
					COUNT(*) FILTER (WHERE status = 'sucesso'),
					COUNT(*) FILTER (WHERE status = 'falha')
				FROM runbook_execution_logs
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval
			`, tenantID, window).Scan(&stats.Automation.SoarExecuted, &stats.Automation.SoarFailed); err != nil {
				return err
			}

			// 4b. Response/containment actions (approved = executed OK, failed = vendor error).
			if err := tx.QueryRow(ctx, `
				SELECT
					COUNT(*) FILTER (WHERE status = 'approved'),
					COUNT(*) FILTER (WHERE status = 'failed')
				FROM response_action_requests
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval
			`, tenantID, window).Scan(&stats.Automation.ResponseExecuted, &stats.Automation.ResponseFailed); err != nil {
				return err
			}
			stats.Automation.EstimatedHoursSaved = estimateHoursSaved(stats.Automation.SoarExecuted, stats.Automation.ResponseExecuted)

			// 4c. Rework/reopen rate (K1): reopened alerts (reopen-comment marker) over total closures
			// in the window. Closures = alerts resolved in the window + the reopens (which were also
			// closed at some point), so the rate reads "of everything closed, what fraction bounced back".
			var resolvedInWindow int
			if err := tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM incident_comments
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval AND comment LIKE $3
			`, tenantID, window, AlertReopenedCommentPrefix+"%").Scan(&stats.Rework.Reopened); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM alerts
				WHERE tenant_id = $1 AND resolved_at >= NOW() - $2::interval
			`, tenantID, window).Scan(&resolvedInWindow); err != nil {
				return err
			}
			stats.Rework.Closed = resolvedInWindow + stats.Rework.Reopened
			stats.Rework.ReopenRatePct = reopenRatePct(stats.Rework.Reopened, stats.Rework.Closed)

			// 4d. SLA-breach escalations paged in the window (escalation-comment marker).
			if err := tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM incident_comments
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval AND comment LIKE $3
			`, tenantID, window, SLAEscalatedCommentPrefix+"%").Scan(&stats.Escalations.SLABreaches); err != nil {
				return err
			}

			// 4e. Detection quality (K5): disposition breakdown of incidents the analyst classified in
			// the window. FP rate = false_positive / total classified.
			if err := tx.QueryRow(ctx, `
				SELECT
					COUNT(*) FILTER (WHERE disposition = 'true_positive'),
					COUNT(*) FILTER (WHERE disposition = 'false_positive'),
					COUNT(*) FILTER (WHERE disposition = 'benign')
				FROM incidents
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval AND disposition IS NOT NULL
			`, tenantID, window).Scan(&stats.Disposition.TruePositive, &stats.Disposition.FalsePositive, &stats.Disposition.Benign); err != nil {
				return err
			}
			stats.Disposition.Classified = stats.Disposition.TruePositive + stats.Disposition.FalsePositive + stats.Disposition.Benign
			stats.Disposition.FalsePositiveRatePct = falsePositiveRatePct(stats.Disposition.FalsePositive, stats.Disposition.Classified)

			// 4f. Detection delay / MTTD (K4): avg (ingested_at - created_at) over alerts whose source
			// carried a usable origin time — a delay in [1s, 7d]. Excludes now≈now (no origin) and
			// clock-skew/backfill garbage. created_at is the origin time; ingested_at the detection time.
			if err := tx.QueryRow(ctx, `
				SELECT
					COUNT(*),
					COUNT(*) FILTER (WHERE ingested_at - created_at BETWEEN interval '1 second' AND interval '7 days'),
					COALESCE(AVG(EXTRACT(EPOCH FROM (ingested_at - created_at))) FILTER (WHERE ingested_at - created_at BETWEEN interval '1 second' AND interval '7 days'), 0)
				FROM alerts
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval
			`, tenantID, window).Scan(&stats.Detection.TotalAlerts, &stats.Detection.Instrumented, &stats.Detection.AvgMTTDSeconds); err != nil {
				return err
			}
			stats.Detection.InstrumentedPct = detectionCoveragePct(stats.Detection.Instrumented, stats.Detection.TotalAlerts)

			// 5. MITRE ATT&CK tactic breakdown (windowed).
			mitreRows, err := tx.Query(ctx, `
				SELECT mitre_tactics, COUNT(*) AS c
				FROM alerts
				WHERE tenant_id = $1 AND created_at >= NOW() - $2::interval
				  AND mitre_tactics IS NOT NULL AND mitre_tactics <> ''
				GROUP BY mitre_tactics
				ORDER BY c DESC
				LIMIT 10
			`, tenantID, window)
			if err != nil {
				return err
			}
			for mitreRows.Next() {
				var m MitreCount
				if err := mitreRows.Scan(&m.Tactic, &m.Count); err != nil {
					mitreRows.Close()
					return err
				}
				stats.ByMitre = append(stats.ByMitre, m)
			}
			mitreRows.Close()
			if err := mitreRows.Err(); err != nil {
				return err
			}

			// Collect active integration types for the heartbeat check below.
			intRows, err := tx.Query(ctx, `
				SELECT DISTINCT type FROM tenant_integrations WHERE tenant_id = $1 AND status = 'active' ORDER BY type
			`, tenantID)
			if err != nil {
				return err
			}
			for intRows.Next() {
				var t string
				if err := intRows.Scan(&t); err != nil {
					intRows.Close()
					return err
				}
				activeTypes = append(activeTypes, t)
			}
			intRows.Close()
			return intRows.Err()
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to compute operational stats: %v", err), http.StatusInternalServerError)
			return
		}

		// 6. Source health: is each active integration still sending heartbeats? (Redis, keyed by
		// tenant; a missing or stale heartbeat flags a silent telemetry source.)
		stats.SourceHealth = resolveSourceHealth(r.Context(), redisClient, tenantID, activeTypes)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

// resolveSourceHealth reads the per-connector heartbeat timestamps from Redis and flags any
// active integration that has gone silent (no heartbeat, or older than the threshold).
func resolveSourceHealth(ctx context.Context, redisClient *redis.Client, tenantID uuid.UUID, activeTypes []string) []SourceHeartbeat {
	out := make([]SourceHeartbeat, 0, len(activeTypes))
	now := time.Now().Unix()
	for _, t := range activeTypes {
		hb := SourceHeartbeat{Type: t, LastSeenSecondsAgo: -1, Silent: true}
		// New uniform key, falling back to the legacy key during the rollout window.
		raw, _ := cache.GetWithLegacyFallback(ctx, redisClient, cache.TenantKey(tenantID, "heartbeat", t), cache.LegacyHeartbeatKey(tenantID, t))
		if val, perr := strconv.ParseInt(raw, 10, 64); perr == nil && val > 0 {
			ago := now - val
			if ago < 0 {
				ago = 0
			}
			hb.LastSeenSecondsAgo = ago
			hb.Silent = ago > silentSourceThresholdSeconds
		}
		out = append(out, hb)
	}
	return out
}
