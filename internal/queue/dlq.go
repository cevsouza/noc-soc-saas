package queue

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// DLQEntry wraps a failed raw payload with enough context to inspect (GET /api/v1/dlq) and
// replay (POST /api/v1/dlq/replay) it later.
type DLQEntry struct {
	Payload    json.RawMessage `json:"payload"`
	Reason     string          `json:"reason"`
	Error      string          `json:"error"`
	FailedAt   time.Time       `json:"failed_at"`
	RetryCount int             `json:"retry_count"`
}

// PushToDLQ appends an entry to the DLQ and trims the list to MaxDLQSize in the same
// pipeline, so the queue can never grow unbounded regardless of how long entries sit unread.
func PushToDLQ(ctx context.Context, rc *redis.Client, entry DLQEntry) error {
	bytes, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	pipe := rc.Pipeline()
	pipe.LPush(ctx, AlertsDLQQueueKey, bytes)
	pipe.LTrim(ctx, AlertsDLQQueueKey, 0, MaxDLQSize-1)
	_, err = pipe.Exec(ctx)
	return err
}
