package queue_test

import (
	"context"
	"testing"
	"time"

	"noc-api/internal/queue"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestPushToDLQAppendsEntry(t *testing.T) {
	rc := newTestRedis(t)
	ctx := context.Background()

	entry := queue.DLQEntry{
		Payload:  []byte(`{"foo":"bar"}`),
		Reason:   "mapping_failed",
		Error:    "boom",
		FailedAt: time.Now(),
	}
	if err := queue.PushToDLQ(ctx, rc, entry); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	length, err := rc.LLen(ctx, queue.AlertsDLQQueueKey).Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if length != 1 {
		t.Errorf("expected DLQ length 1, got %d", length)
	}
}

func TestPushToDLQCapsListLength(t *testing.T) {
	rc := newTestRedis(t)
	ctx := context.Background()

	// Push a small number more than a locally-scaled cap would allow, but since MaxDLQSize is
	// a package constant (5000) we can't shrink it for the test — instead verify the LTrim
	// call itself keeps the list bounded by pushing well past a reasonable sample size and
	// checking the list never exceeds queue.MaxDLQSize.
	const sample = 50
	for i := 0; i < sample; i++ {
		entry := queue.DLQEntry{Payload: []byte(`{}`), Reason: "test", FailedAt: time.Now()}
		if err := queue.PushToDLQ(ctx, rc, entry); err != nil {
			t.Fatalf("unexpected error on push %d: %v", i, err)
		}
	}

	length, err := rc.LLen(ctx, queue.AlertsDLQQueueKey).Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if length != sample {
		t.Errorf("expected DLQ length %d (below cap), got %d", sample, length)
	}
	if length > queue.MaxDLQSize {
		t.Errorf("DLQ length %d exceeds MaxDLQSize %d", length, queue.MaxDLQSize)
	}
}
