package worker

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// suppressionRule is the worker-side projection of a tenant_suppression_rules row.
type suppressionRule struct {
	MatchField string     `json:"match_field"`
	MatchValue string     `json:"match_value"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
}

// ruleMatches reports whether a rule suppresses an event with the given fields at time `now`. Pure
// and unit-tested. A rule matches when `now` is within its optional [starts_at, ends_at] window and
// the event's match_field contains (case-insensitive) the rule's match_value.
func ruleMatches(r suppressionRule, fields map[string]string, now time.Time) bool {
	if r.StartsAt != nil && now.Before(*r.StartsAt) {
		return false
	}
	if r.EndsAt != nil && now.After(*r.EndsAt) {
		return false
	}
	v, ok := fields[r.MatchField]
	if !ok || r.MatchValue == "" {
		return false
	}
	return strings.Contains(strings.ToLower(v), strings.ToLower(r.MatchValue))
}

// eventSuppressed reports whether ANY rule suppresses the event. Pure.
func eventSuppressed(rules []suppressionRule, fields map[string]string, now time.Time) bool {
	for _, r := range rules {
		if ruleMatches(r, fields, now) {
			return true
		}
	}
	return false
}

// suppressionCacheTTL bounds how stale the per-tenant rule cache can be. On rule create/delete the
// API deletes the cache key so changes take effect at once; this TTL just caps drift otherwise.
const suppressionCacheTTL = 30 * time.Second

// loadSuppressionRules returns the tenant's active suppression rules, cached in Redis so the worker
// doesn't hit Postgres on every single event. Must be called with a tenant-scoped context.
func (wp *WorkerPool) loadSuppressionRules(ctx context.Context, tenantID uuid.UUID) []suppressionRule {
	key := "suppression:rules:" + tenantID.String()
	if cached, err := wp.redisClient.Get(ctx, key).Result(); err == nil {
		var rules []suppressionRule
		if json.Unmarshal([]byte(cached), &rules) == nil {
			return rules
		}
	}

	rules := []suppressionRule{}
	_ = db.ExecuteInTenantTx(ctx, wp.pgPool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT match_field, match_value, starts_at, ends_at
			FROM tenant_suppression_rules
			WHERE tenant_id = $1 AND active = TRUE
		`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r suppressionRule
			if err := rows.Scan(&r.MatchField, &r.MatchValue, &r.StartsAt, &r.EndsAt); err != nil {
				return err
			}
			rules = append(rules, r)
		}
		return rows.Err()
	})

	if b, err := json.Marshal(rules); err == nil {
		_ = wp.redisClient.Set(ctx, key, b, suppressionCacheTTL).Err()
	}
	return rules
}
