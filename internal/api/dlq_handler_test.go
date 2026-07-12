package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/queue"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newDLQTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func pushDLQEntry(t *testing.T, rc *redis.Client, entry queue.DLQEntry) {
	t.Helper()
	if err := queue.PushToDLQ(context.Background(), rc, entry); err != nil {
		t.Fatalf("failed to seed DLQ entry: %v", err)
	}
}

func TestHandleGetDLQIsNonDestructive(t *testing.T) {
	rc := newDLQTestRedis(t)
	ctx := context.Background()

	pushDLQEntry(t, rc, queue.DLQEntry{Payload: []byte(`{"a":1}`), Reason: "test", FailedAt: time.Now()})
	pushDLQEntry(t, rc, queue.DLQEntry{Payload: []byte(`{"b":2}`), Reason: "test", FailedAt: time.Now()})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dlq?limit=10", nil)
	rec := httptest.NewRecorder()
	api.HandleGetDLQ(rc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp api.DLQListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Total != 2 || len(resp.Items) != 2 {
		t.Errorf("expected total=2 and 2 items, got total=%d items=%d", resp.Total, len(resp.Items))
	}

	// Peeking must not remove anything.
	length, err := rc.LLen(ctx, queue.AlertsDLQQueueKey).Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if length != 2 {
		t.Errorf("expected DLQ length unchanged at 2 after GET, got %d", length)
	}
}

func TestHandleReplayDLQ(t *testing.T) {
	rc := newDLQTestRedis(t)
	ctx := context.Background()

	// 1. A well-formed raw message for an unregistered integration type: MapRawPayload falls
	// back to a generic passthrough incident, so this should succeed and land on the
	// normalized queue.
	okRawMsg := queue.RawWebhookMessage{
		TenantID:        uuid.New(),
		IntegrationType: "unknown_tool",
		Payload:         map[string]interface{}{"foo": "bar"},
	}
	okPayload, _ := json.Marshal(okRawMsg)
	pushDLQEntry(t, rc, queue.DLQEntry{Payload: okPayload, Reason: "mapping_failed", FailedAt: time.Now(), RetryCount: 0})

	// 2. An entry whose Payload is syntactically valid JSON (a quoted string) but the wrong
	// shape to unmarshal into queue.RawWebhookMessage, with RetryCount already at
	// MaxDLQRetries-1: one more failed attempt should push it over the threshold and into the
	// poison queue instead of back to the DLQ.
	pushDLQEntry(t, rc, queue.DLQEntry{Payload: []byte(`"malformed-1"`), Reason: "malformed_json", FailedAt: time.Now(), RetryCount: queue.MaxDLQRetries - 1})

	// 3. Same shape mismatch, starting at RetryCount 0: should be requeued to the DLQ with
	// RetryCount incremented to 1, not yet poisoned.
	pushDLQEntry(t, rc, queue.DLQEntry{Payload: []byte(`"malformed-2"`), Reason: "malformed_json", FailedAt: time.Now(), RetryCount: 0})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dlq/replay?limit=10", nil)
	rec := httptest.NewRecorder()
	api.HandleReplayDLQ(rc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp api.DLQReplayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Replayed != 1 {
		t.Errorf("expected 1 successful replay, got %d", resp.Replayed)
	}
	if resp.Poisoned != 1 {
		t.Errorf("expected 1 poisoned entry, got %d", resp.Poisoned)
	}
	if resp.Requeued != 1 {
		t.Errorf("expected 1 requeued entry, got %d", resp.Requeued)
	}
	if resp.RemainingInDLQ != 1 {
		t.Errorf("expected 1 entry remaining in DLQ (the requeued one), got %d", resp.RemainingInDLQ)
	}

	normalizedLen, _ := rc.LLen(ctx, queue.AlertsNormalizedQueueKey).Result()
	if normalizedLen != 1 {
		t.Errorf("expected 1 incident pushed to normalized queue, got %d", normalizedLen)
	}

	poisonLen, _ := rc.LLen(ctx, queue.AlertsPoisonQueueKey).Result()
	if poisonLen != 1 {
		t.Errorf("expected 1 entry in poison queue, got %d", poisonLen)
	}

	// The requeued entry must have its RetryCount incremented, not reset.
	remaining, err := rc.LRange(ctx, queue.AlertsDLQQueueKey, 0, -1).Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected exactly 1 remaining DLQ entry, got %d", len(remaining))
	}
	var requeuedEntry queue.DLQEntry
	if err := json.Unmarshal([]byte(remaining[0]), &requeuedEntry); err != nil {
		t.Fatalf("failed to decode requeued entry: %v", err)
	}
	if requeuedEntry.RetryCount != 1 {
		t.Errorf("expected requeued entry RetryCount=1, got %d", requeuedEntry.RetryCount)
	}
}
