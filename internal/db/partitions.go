package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureAlertPartitions calls the create_alerts_partition_if_needed() Postgres function
// (provisioned by migration 000015) for the current month and the next month, so the
// range-partitioned alerts table never runs out of partitions to insert into even if the app
// doesn't redeploy for a while. Safe to call on every boot — the function itself is a no-op if
// the partition already exists. Requires the table-owner pool (DDL), not the restricted
// noc_app_runtime runtime role.
func EnsureAlertPartitions(ctx context.Context, pool *pgxpool.Pool) error {
	now := time.Now().UTC()
	nextMonth := now.AddDate(0, 1, 0)

	for _, month := range []time.Time{now, nextMonth} {
		if _, err := pool.Exec(ctx, "SELECT create_alerts_partition_if_needed($1)", month); err != nil {
			return fmt.Errorf("failed to ensure alerts partition for %s: %w", month.Format("2006-01"), err)
		}
	}

	log.Println("[DATABASE INFO] Alerts partitions verified for current and next month.")
	return nil
}
