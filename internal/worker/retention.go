package worker

import (
	"context"
	"errors"
	"log"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/retention"

	"github.com/jackc/pgx/v5"
)

// Alert-retention enforcer (Fase 5): periodically deletes each tenant's alerts older than its
// configured retention window. Opt-in — only tenants with a tenant_retention row are touched; the
// hard 30-day floor lives in internal/retention.Cutoff. Mirrors the watchdog/SLA monitor structure.
const retentionScanInterval = 6 * time.Hour

// StartRetentionEnforcer launches the background retention job. The first pass runs on the first
// tick (not at boot) so a rollout never triggers deletions instantly.
func (wp *WorkerPool) StartRetentionEnforcer(ctx context.Context) {
	log.Printf("[Retention] Alert-retention enforcer started (interval=%s, floor=%dd)", retentionScanInterval, retention.MinDays)
	ticker := time.NewTicker(retentionScanInterval)
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
				wp.enforceRetention(ctx)
			}
		}
	}()
}

// enforceRetention iterates active tenants and, for each that has a retention policy, deletes its
// alerts past the window inside that tenant's RLS context. Tenants without a policy are skipped.
func (wp *WorkerPool) enforceRetention(ctx context.Context) {
	tenantIDs, err := wp.activeTenantIDs(ctx)
	if err != nil {
		log.Printf("[Retention] Failed to list active tenants: %v", err)
		return
	}
	for _, tid := range tenantIDs {
		tctx := db.WithTenantID(ctx, tid)
		var days int
		var enabled bool
		var deleted int64
		err := db.ExecuteInTenantTx(tctx, wp.pgPool, func(tx pgx.Tx) error {
			e := tx.QueryRow(tctx, `SELECT alerts_retention_days FROM tenant_retention WHERE tenant_id = $1`, tid).Scan(&days)
			if errors.Is(e, pgx.ErrNoRows) {
				return nil // no policy — keep forever
			}
			if e != nil {
				return e
			}
			enabled = true
			deleted, e = retention.EnforceForTenant(tctx, tx, tid, days)
			return e
		})
		if err != nil {
			log.Printf("[Retention] Enforcement failed for tenant %s: %v", tid, err)
			continue
		}
		if enabled && deleted > 0 {
			log.Printf("[Retention] Tenant %s: deleted %d alerts older than %dd", tid, deleted, days)
		}
	}
}
