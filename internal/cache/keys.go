// Package cache defines the canonical Redis key convention for per-tenant cache/state keys.
//
// Before this, per-tenant keys used ad-hoc prefixes scattered across the codebase
// (heartbeat:connector:<t>:..., webhook:error:<t>:..., ratelimit:tenant:<t>:..., ingest:breaker:...,
// suppression:..., sla:escalated:..., watchdog:alarmed:...). That made it impossible to reason about
// or wipe all of a tenant's ephemeral state in one place. Every per-tenant key now lives under a
// single uniform prefix — tenant:<uuid>:... — so a tenant's cache footprint is discoverable and can
// be cleared wholesale (e.g. on offboarding) via WipeTenant.
//
// Non-tenant-addressable keys stay as they are and are intentionally NOT under this prefix: the
// shared work queues (noc:queue:*), the fingerprint-keyed dedupe debounce (noc:debounce:v2:*), the
// API-key-hash cache (noc:cache:apikey:*), and the cross-instance pub/sub channel (noc:pubsub:*).
package cache

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// TenantPrefix is the single namespace all per-tenant cache/state keys share.
const TenantPrefix = "tenant:"

// TenantKey builds a per-tenant key: TenantKey(id, "heartbeat", "zabbix") -> "tenant:<id>:heartbeat:zabbix".
func TenantKey(tenantID uuid.UUID, parts ...string) string {
	var b strings.Builder
	b.WriteString(TenantPrefix)
	b.WriteString(tenantID.String())
	for _, p := range parts {
		b.WriteByte(':')
		b.WriteString(p)
	}
	return b.String()
}

// LegacyHeartbeatKey is the pre-uniform-prefix heartbeat key. Kept only so readers can fall back to
// it during the rollout window while old-format keys age out (24h TTL). Remove once expired.
func LegacyHeartbeatKey(tenantID uuid.UUID, source string) string {
	return "heartbeat:connector:" + tenantID.String() + ":" + source
}

// GetWithLegacyFallback reads newKey, falling back to legacyKey when newKey is absent. Transitional:
// lets a key rename deploy without a read gap. Once all legacy keys have expired the fallback is dead
// and can be dropped.
func GetWithLegacyFallback(ctx context.Context, rc *redis.Client, newKey, legacyKey string) (string, error) {
	v, err := rc.Get(ctx, newKey).Result()
	if err == nil {
		return v, nil
	}
	return rc.Get(ctx, legacyKey).Result()
}

// TenantScanPattern is the glob matching every key owned by a tenant, for SCAN.
func TenantScanPattern(tenantID uuid.UUID) string {
	return TenantPrefix + tenantID.String() + ":*"
}

// WipeTenant best-effort deletes every per-tenant cache/state key for a tenant (used on offboarding).
// It SCANs rather than KEYS to avoid blocking Redis, and deletes in batches. Returns the count removed.
func WipeTenant(ctx context.Context, rc *redis.Client, tenantID uuid.UUID) (int, error) {
	pattern := TenantScanPattern(tenantID)
	var cursor uint64
	removed := 0
	for {
		keys, next, err := rc.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return removed, err
		}
		if len(keys) > 0 {
			if err := rc.Del(ctx, keys...).Err(); err != nil {
				return removed, err
			}
			removed += len(keys)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return removed, nil
}
