// Package retention holds the per-tenant alert retention policy logic (Fase 5), shared by the API
// (config + manual enforce) and the background enforcement worker so the cutoff rule and delete
// query live in exactly one place.
package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// MinDays is a hard floor: no retention policy may delete alerts younger than this, enforced both
	// by the tenant_retention CHECK constraint and again here as defense-in-depth.
	MinDays = 30
	// MaxDays caps how far out a policy may be set (~10 years).
	MaxDays = 3650
)

// ValidateDays checks a proposed retention window (pure).
func ValidateDays(days int) error {
	if days < MinDays {
		return fmt.Errorf("alerts_retention_days must be at least %d", MinDays)
	}
	if days > MaxDays {
		return fmt.Errorf("alerts_retention_days must be at most %d", MaxDays)
	}
	return nil
}

// Cutoff returns the timestamp before which alerts are eligible for deletion, clamping the window up
// to the MinDays floor so a value below the floor can never widen the deletion. Pure and unit-tested.
func Cutoff(now time.Time, retentionDays int) time.Time {
	if retentionDays < MinDays {
		retentionDays = MinDays
	}
	return now.AddDate(0, 0, -retentionDays)
}

// EnforceForTenant deletes the tenant's alerts older than its retention window. Must run inside the
// tenant's RLS transaction. Returns the number of alerts deleted. The explicit tenant_id predicate
// is defense-in-depth on top of RLS (same posture as the rest of the codebase).
func EnforceForTenant(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, retentionDays int) (int64, error) {
	cutoff := Cutoff(time.Now(), retentionDays)
	tag, err := tx.Exec(ctx, `DELETE FROM alerts WHERE tenant_id = $1 AND created_at < $2`, tenantID, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// EnforceIncidentsForTenant deletes the tenant's RESOLVED incidents older than the retention window
// (B4). Only resolved incidents are pruned — an open investigation is never deleted regardless of
// age. Audit logs are deliberately NOT pruned (append-only compliance record; kept indefinitely).
func EnforceIncidentsForTenant(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, retentionDays int) (int64, error) {
	cutoff := Cutoff(time.Now(), retentionDays)
	tag, err := tx.Exec(ctx, `DELETE FROM incidents WHERE tenant_id = $1 AND status = 'resolved' AND created_at < $2`, tenantID, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
