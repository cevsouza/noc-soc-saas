package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"
	"noc-api/internal/queue"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Per-tenant ingestion circuit breaker (Fase 7 — resiliência / isolamento de ingestão).
//
// The per-tenant RateLimiter already caps steady traffic (429 above N req/min), but it re-evaluates
// every request forever — a tenant hammering far past the limit keeps hitting Redis and the auth
// chain on every call, and a genuine flood can still pile events onto the SHARED normalized queue
// faster than the worker drains it, starving well-behaved tenants. A circuit breaker adds the
// missing behavior: once a tenant sustains a flood past a (higher) burst threshold, the breaker
// OPENS and sheds that tenant's ingestion cheaply for a cooldown window — protecting the shared
// downstream — then auto-closes when the cooldown lapses (a fresh flood re-trips it).
//
// It also raises ONE first-class alarm on the trip transition (a source=system event pushed through
// the normal pipeline, so it escalates via the configured channels but skips SOAR), making the flood
// observable instead of a silent 503 storm. The alarm is produced internally and is never itself
// shed.
//
// Scope note: this keys off the tenant that APIKeyAuth resolves into the request context, so it
// guards the API-key ingest routes. The generic HMAC webhook resolves its tenant from the URL path
// inside the handler (not context) and rides the separate raw→mapping async path with its own DLQ,
// so it is intentionally out of this breaker's scope in v1.

const (
	defaultIngestBreakerThreshold = 1500 // req/min per tenant that trips the breaker (well above the 500/min steady rate limit)
	defaultIngestBreakerCooldown  = 300  // seconds the breaker stays open (shedding) after tripping
)

func ingestEnvInt(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// IngestCircuitBreaker returns middleware that sheds a flooding tenant's ingestion. Place it AFTER
// APIKeyAuth (which sets the tenant in context) and BEFORE the RateLimiter/handler.
func IngestCircuitBreaker(redisClient *redis.Client) func(http.Handler) http.Handler {
	threshold := ingestEnvInt("INGEST_BREAKER_THRESHOLD", defaultIngestBreakerThreshold)
	cooldown := ingestEnvInt("INGEST_BREAKER_COOLDOWN_SECONDS", defaultIngestBreakerCooldown)
	log.Printf("[IngestBreaker] Per-tenant ingestion circuit breaker enabled (threshold=%d/min cooldown=%ds)", threshold, cooldown)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, ok := db.TenantIDFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r) // no resolved tenant (e.g. webhook path) — nothing to break on
				return
			}
			ctx := r.Context()
			openKey := "ingest:breaker:open:" + tenantID.String()
			shedKey := "ingest:breaker:shed:" + tenantID.String()

			// Breaker already open → shed cheaply without touching the counter/handler.
			if _, err := redisClient.Get(ctx, openKey).Result(); err == nil {
				redisClient.Incr(ctx, shedKey)
				shed(w, cooldown, "open")
				return
			}

			// Count this request in the current-minute window.
			minute := time.Now().Unix() / 60
			countKey := fmt.Sprintf("ingest:breaker:count:%s:%d", tenantID.String(), minute)
			pipe := redisClient.Pipeline()
			incr := pipe.Incr(ctx, countKey)
			pipe.Expire(ctx, countKey, 120*time.Second)
			if _, err := pipe.Exec(ctx); err != nil {
				next.ServeHTTP(w, r) // fail open — never block telemetry on a Redis hiccup
				return
			}

			if incr.Val() > threshold {
				// Trip. SetNX makes exactly one request (per cooldown window) the one that opens the
				// breaker and raises the single alarm; the rest just shed.
				set, _ := redisClient.SetNX(ctx, openKey, time.Now().Unix(), time.Duration(cooldown)*time.Second).Result()
				if set {
					raiseIngestBreakerAlarm(ctx, redisClient, tenantID, incr.Val(), threshold)
					log.Printf("[IngestBreaker] OPEN tenant=%s count=%d threshold=%d cooldown=%ds", tenantID, incr.Val(), threshold, cooldown)
				}
				redisClient.Incr(ctx, shedKey)
				shed(w, cooldown, "tripped")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func shed(w http.ResponseWriter, cooldown int64, reason string) {
	w.Header().Set("Retry-After", strconv.FormatInt(cooldown, 10))
	http.Error(w, "Service Unavailable: ingestion circuit breaker "+reason+" for this tenant (flood protection)", http.StatusServiceUnavailable)
}

// raiseIngestBreakerAlarm injects a synthetic system alert onto the normalized queue so the worker
// turns the trip into a first-class, escalatable alert (source=system → escalates, skips SOAR). A
// stable ExternalID makes repeat trips dedupe/group into the same incident.
func raiseIngestBreakerAlarm(ctx context.Context, rc *redis.Client, tenantID uuid.UUID, count, threshold int64) {
	inc := model.UnifiedIncident{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Source:     model.SourceSystem,
		EventType:  "ingestion_breaker_open",
		Severity:   model.SeverityCritical,
		Title:      fmt.Sprintf("Circuit breaker de ingestão ABERTO: %d req/min excederam o limite de %d — tráfego deste tenant está sendo descartado temporariamente para proteger a fila compartilhada", count, threshold),
		Timestamp:  time.Now(),
		Host:       "ingestion-breaker",
		ExternalID: "ingest-breaker:" + tenantID.String(),
	}
	b, err := json.Marshal(inc)
	if err != nil {
		return
	}
	if err := rc.LPush(ctx, queue.AlertsNormalizedQueueKey, b).Err(); err != nil {
		log.Printf("[IngestBreaker] Failed to enqueue breaker alarm for tenant %s: %v", tenantID, err)
	}
}
