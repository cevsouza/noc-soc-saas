package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"noc-api/internal/db"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func breakerTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// serve runs one request through the breaker with a tenant already in context, returning the status
// code and whether the downstream handler was reached.
func serve(t *testing.T, rc *redis.Client, tenantID uuid.UUID) (int, bool) {
	t.Helper()
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	h := IngestCircuitBreaker(rc)(next)
	req := httptest.NewRequest("POST", "/api/v1/ingest/zabbix", nil)
	req = req.WithContext(db.WithTenantID(req.Context(), tenantID))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code, reached
}

func TestIngestBreaker_PassesUnderThreshold(t *testing.T) {
	rc := breakerTestRedis(t)
	code, reached := serve(t, rc, uuid.New())
	if code != http.StatusOK || !reached {
		t.Fatalf("under threshold: got code=%d reached=%v, want 200/true", code, reached)
	}
}

func TestIngestBreaker_TripsAndSheds(t *testing.T) {
	t.Setenv("INGEST_BREAKER_THRESHOLD", "3")
	rc := breakerTestRedis(t)
	tenantID := uuid.New()

	// First 3 requests pass (count 1,2,3 — not > threshold).
	for i := 0; i < 3; i++ {
		if code, reached := serve(t, rc, tenantID); code != http.StatusOK || !reached {
			t.Fatalf("request %d should pass: code=%d reached=%v", i+1, code, reached)
		}
	}
	// 4th trips the breaker (count 4 > 3) → shed, downstream not reached.
	if code, reached := serve(t, rc, tenantID); code != http.StatusServiceUnavailable || reached {
		t.Fatalf("trip request: got code=%d reached=%v, want 503/false", code, reached)
	}
	// Breaker now open → subsequent requests shed cheaply too.
	if code, reached := serve(t, rc, tenantID); code != http.StatusServiceUnavailable || reached {
		t.Fatalf("post-trip request: got code=%d reached=%v, want 503/false", code, reached)
	}

	// A different tenant is unaffected (per-tenant isolation).
	if code, reached := serve(t, rc, uuid.New()); code != http.StatusOK || !reached {
		t.Fatalf("other tenant should pass: code=%d reached=%v", code, reached)
	}
}
