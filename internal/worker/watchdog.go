package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Connection watchdog — a dead-man's-switch that detects when an *active* telemetry integration
// stops sending data (or never started) and raises a first-class alert so the operator is paged
// instead of silently going blind. This is the "não ficar desconectado sem alarme" guarantee.
//
// Design notes / why it is driven by tenant_integrations and not by leftover Redis keys:
//   - The source of truth for "what SHOULD be reporting" is `tenant_integrations` with
//     status='active'. Scanning stray `heartbeat:connector:*` Redis keys (the old approach) both
//     false-alarmed on integrations that had been deactivated/deleted (their heartbeat key lingers
//     up to 24h) and completely missed the most important onboarding failure: an integration that
//     was activated but never sent a single heartbeat (misconfigured from day one) has no key at
//     all, so a key-scan can never flag it. Driving from active integrations fixes both.
//   - Only inbound telemetry sources are watched. Pure-outbound escalation channels (slack/teams/
//     email) never ingest, and the dual pagerduty/opsgenie are commonly used outbound-only, so
//     watching them would false-alarm; they are excluded.
//   - A per-source Redis state flag suppresses re-alarming a persistent outage more often than the
//     re-notify interval, and is cleared on recovery so the next outage pages again.
//
// Honest limitation (documented, not hidden): heartbeats are written only when data arrives, so a
// legitimately quiet source (e.g. a firewall with no threats for a while) is indistinguishable
// from a disconnected one. The silence threshold is therefore a tunable dead-man's-switch, not a
// perfect liveness probe. A per-integration expected-interval is a planned refinement (A1
// follow-up); for now one global threshold applies, overridable via env.

const (
	watchdogScanInterval        = 60 * time.Second
	defaultSilenceThresholdSecs = 600  // 10 min without any telemetry → considered silent
	defaultGraceSecs            = 900  // 15 min after activation before a never-connected source alarms
	defaultRenotifySecs         = 3600 // don't re-alarm the same persistent outage more than hourly
)

// watchdogMonitoredTypes is the set of inbound telemetry integration types the watchdog treats as
// "should be reporting". Mirrors the inbound connectors in internal/connector plus the poll
// connectors (loki/sentinel); excludes outbound-only escalation channels. Keep in sync when a new
// inbound connector is added.
var watchdogMonitoredTypes = []string{
	"zabbix", "prometheus", "uptimekuma", "wazuh", "grafana", "sentinel", "loki",
	"otlp", "icinga", "azuremonitor", "cloudwatch", "crowdstrike", "paloalto", "fortinet",
}

// watchdogDecision is the outcome of evaluating one source's health.
type watchdogDecision int

const (
	decisionNone    watchdogDecision = iota // healthy, or too new / suppressed — do nothing
	decisionAlarm                           // silent past threshold and not already alarmed — page
	decisionRecover                         // was alarmed, now healthy — clear state, log recovery
)

// evaluateSource is the pure decision core (unit-tested without Redis/Postgres). Given the current
// time, the source's last heartbeat (and whether one exists), when the integration was activated,
// whether it is already in the alarmed state, and the thresholds, it returns what to do.
func evaluateSource(now, lastSeen int64, hasHeartbeat bool, createdAt int64, alarmed bool, silenceThreshold, graceSecs int64) watchdogDecision {
	var silent bool
	if hasHeartbeat {
		silent = now-lastSeen > silenceThreshold
	} else {
		// Never sent a heartbeat: only alarm once past the post-activation grace window, so an
		// admin still finishing onboarding isn't paged the instant they flip the integration on.
		silent = now-createdAt > graceSecs
	}

	switch {
	case silent && !alarmed:
		return decisionAlarm
	case !silent && alarmed:
		return decisionRecover
	default:
		return decisionNone
	}
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// StartWatchdog launches the background connection-health checker.
func (wp *WorkerPool) StartWatchdog(ctx context.Context) {
	silenceThreshold := envInt64("WATCHDOG_SILENCE_SECONDS", defaultSilenceThresholdSecs)
	graceSecs := envInt64("WATCHDOG_GRACE_SECONDS", defaultGraceSecs)
	renotifySecs := envInt64("WATCHDOG_RENOTIFY_SECONDS", defaultRenotifySecs)
	log.Printf("[Watchdog] Connection watchdog started (silence=%ds grace=%ds renotify=%ds)", silenceThreshold, graceSecs, renotifySecs)

	ticker := time.NewTicker(watchdogScanInterval)
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
				wp.scanConnections(ctx, silenceThreshold, graceSecs, renotifySecs)
			}
		}
	}()
}

// scanConnections iterates every active tenant, then that tenant's active inbound integrations,
// and evaluates each source's heartbeat freshness. The tenants table is not RLS-forced (it is the
// tenant registry itself), so it can be read directly; per-tenant integration reads run inside the
// tenant's RLS context.
func (wp *WorkerPool) scanConnections(ctx context.Context, silenceThreshold, graceSecs, renotifySecs int64) {
	tenantIDs, err := wp.activeTenantIDs(ctx)
	if err != nil {
		log.Printf("[Watchdog] Failed to list active tenants: %v", err)
		return
	}
	now := time.Now().Unix()
	for _, tenantID := range tenantIDs {
		sources, err := wp.activeInboundIntegrations(ctx, tenantID)
		if err != nil {
			log.Printf("[Watchdog] Failed to list integrations for tenant %s: %v", tenantID, err)
			continue
		}
		for _, s := range sources {
			wp.evaluateAndAct(ctx, tenantID, s.itype, s.createdAt.Unix(), now, silenceThreshold, graceSecs, renotifySecs)
		}
	}
}

func (wp *WorkerPool) activeTenantIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := wp.pgPool.Query(ctx, `SELECT id FROM tenants WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type watchedSource struct {
	itype     string
	createdAt time.Time
}

func (wp *WorkerPool) activeInboundIntegrations(ctx context.Context, tenantID uuid.UUID) ([]watchedSource, error) {
	tenantCtx := db.WithTenantID(ctx, tenantID)
	var out []watchedSource
	err := db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
		rows, err := tx.Query(tenantCtx, `
			SELECT type, created_at
			FROM tenant_integrations
			WHERE tenant_id = $1 AND status = 'active' AND type = ANY($2)
		`, tenantID, watchdogMonitoredTypes)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s watchedSource
			if err := rows.Scan(&s.itype, &s.createdAt); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	return out, err
}

// evaluateAndAct reads the heartbeat + alarmed state for one source and acts on the decision.
func (wp *WorkerPool) evaluateAndAct(ctx context.Context, tenantID uuid.UUID, itype string, createdAt, now, silenceThreshold, graceSecs, renotifySecs int64) {
	heartbeatKey := "heartbeat:connector:" + tenantID.String() + ":" + itype
	alarmKey := "watchdog:alarmed:" + tenantID.String() + ":" + itype

	var lastSeen int64
	hasHeartbeat := false
	if v, err := wp.redisClient.Get(ctx, heartbeatKey).Result(); err == nil && v != "" {
		if n, perr := strconv.ParseInt(v, 10, 64); perr == nil {
			lastSeen = n
			hasHeartbeat = true
		}
	}

	alarmed := false
	if _, err := wp.redisClient.Get(ctx, alarmKey).Result(); err == nil {
		alarmed = true
	}

	switch evaluateSource(now, lastSeen, hasHeartbeat, createdAt, alarmed, silenceThreshold, graceSecs) {
	case decisionAlarm:
		wp.raiseTelemetryLossAlarm(ctx, tenantID, itype, hasHeartbeat, now-lastSeen)
		// Suppress re-alarming this same outage until the re-notify window elapses.
		_ = wp.redisClient.Set(ctx, alarmKey, now, time.Duration(renotifySecs)*time.Second).Err()
	case decisionRecover:
		_ = wp.redisClient.Del(ctx, alarmKey).Err()
		log.Printf("[Watchdog] Recovery: connector %s for tenant %s is reporting again", itype, tenantID)
	case decisionNone:
		// healthy, too new, or still within a suppression window — nothing to do.
	}
}

// raiseTelemetryLossAlarm injects a synthetic system alert that flows through the normal pipeline
// (dedupe → persist → WebSocket → escalations). Because Source=system, createNewAlert pages the
// configured channels but deliberately skips SOAR remediation.
func (wp *WorkerPool) raiseTelemetryLossAlarm(ctx context.Context, tenantID uuid.UUID, itype string, hasHeartbeat bool, silentFor int64) {
	var summary string
	if hasHeartbeat {
		summary = fmt.Sprintf("Perda de telemetria: o conector '%s' parou de enviar dados há %dm (possível desconexão)", itype, silentFor/60)
	} else {
		summary = fmt.Sprintf("Conector '%s' ativado mas nunca enviou telemetria — verifique a configuração da integração", itype)
	}

	incident := model.UnifiedIncident{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Source:     model.SourceSystem,
		EventType:  "telemetry_loss_" + itype,
		Severity:   model.SeverityCritical,
		Title:      summary,
		Timestamp:  time.Now(),
		Host:       "connection-watchdog",
		ExternalID: "watchdog:" + itype, // stable per source → dedupe groups repeats of the same outage
	}
	if err := wp.processEvent(ctx, incident); err != nil {
		log.Printf("[Watchdog] Failed to raise telemetry-loss alarm for %s/%s: %v", tenantID, itype, err)
		return
	}
	log.Printf("[Watchdog] ALARM raised: connector %s silent for tenant %s (hasHeartbeat=%t)", itype, tenantID, hasHeartbeat)
}
