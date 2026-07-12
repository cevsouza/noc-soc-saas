package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"noc-api/internal/connector"
	"noc-api/internal/model"
	"noc-api/internal/queue"

	"github.com/redis/go-redis/v9"
)

type DLQListResponse struct {
	Total int              `json:"total"`
	Items []queue.DLQEntry `json:"items"`
}

// HandleGetDLQ peeks at the dead-letter queue without removing anything (LRange, not RPOP), so
// it's safe to call repeatedly while investigating a stuck integration.
func HandleGetDLQ(redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		ctx := r.Context()
		total, err := redisClient.LLen(ctx, queue.AlertsDLQQueueKey).Result()
		if err != nil {
			http.Error(w, "Internal Server Error: failed to read DLQ length", http.StatusInternalServerError)
			return
		}

		rawEntries, err := redisClient.LRange(ctx, queue.AlertsDLQQueueKey, 0, int64(limit-1)).Result()
		if err != nil {
			http.Error(w, "Internal Server Error: failed to read DLQ entries", http.StatusInternalServerError)
			return
		}

		items := make([]queue.DLQEntry, 0, len(rawEntries))
		for _, raw := range rawEntries {
			var entry queue.DLQEntry
			if err := json.Unmarshal([]byte(raw), &entry); err != nil {
				continue
			}
			items = append(items, entry)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DLQListResponse{Total: int(total), Items: items})
	}
}

type DLQReplayResponse struct {
	Replayed       int `json:"replayed"`
	Requeued       int `json:"requeued"`
	Poisoned       int `json:"poisoned"`
	RemainingInDLQ int `json:"remaining_in_dlq"`
}

// HandleReplayDLQ pops up to `limit` entries off the DLQ (oldest first, matching the LPush/RPOP
// FIFO convention used by the rest of the ingestion pipeline) and attempts to remap each one
// directly (via connector.MapRawPayload, the same dispatch the async mapping engine uses)
// rather than pushing it back onto the raw queue — remapping it here, in the same request,
// keeps the RetryCount attached to this specific entry so MaxDLQRetries is actually enforced
// (round-tripping through the raw queue would lose that count: the mapping engine's failure
// path always starts a fresh DLQEntry at RetryCount 0, with no memory of prior attempts).
// Successfully remapped incidents go straight to the normalized queue; failures increment
// RetryCount and either go back to the DLQ or, once MaxDLQRetries is reached, to the poison
// queue. There is no automatic background replay by design: this is an explicit admin action,
// so a persistently broken tenant integration surfaces for a human to fix rather than looping
// silently.
func HandleReplayDLQ(redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		ctx := r.Context()
		replayed, requeued, poisoned := 0, 0, 0

		// Only process entries that existed at the start of this call — otherwise an entry
		// requeued back onto the DLQ (RetryCount incremented) could be popped again within
		// the same request and double-counted toward MaxDLQRetries.
		initialLen, err := redisClient.LLen(ctx, queue.AlertsDLQQueueKey).Result()
		if err != nil {
			http.Error(w, "Internal Server Error: failed to read DLQ length", http.StatusInternalServerError)
			return
		}
		iterations := limit
		if int(initialLen) < iterations {
			iterations = int(initialLen)
		}

		for i := 0; i < iterations; i++ {
			raw, err := redisClient.RPop(ctx, queue.AlertsDLQQueueKey).Result()
			if err == redis.Nil {
				break
			}
			if err != nil {
				http.Error(w, "Internal Server Error: failed to pop DLQ entry", http.StatusInternalServerError)
				return
			}

			var entry queue.DLQEntry
			if err := json.Unmarshal([]byte(raw), &entry); err != nil {
				// Can't even parse the DLQ envelope itself — drop it rather than loop forever.
				continue
			}

			var rawMsg queue.RawWebhookMessage
			mapErr := json.Unmarshal(entry.Payload, &rawMsg)
			if mapErr == nil {
				var mapped []model.UnifiedIncident
				mapped, mapErr = connector.MapRawPayload(rawMsg.IntegrationType, rawMsg.Payload, rawMsg.TenantID)
				if mapErr == nil {
					for _, inc := range mapped {
						incidentBytes, _ := json.Marshal(inc)
						redisClient.LPush(ctx, queue.AlertsNormalizedQueueKey, incidentBytes)
					}
					replayed++
					continue
				}
			}

			entry.RetryCount++
			entry.Error = mapErr.Error()
			if entry.RetryCount >= queue.MaxDLQRetries {
				poisonBytes, _ := json.Marshal(entry)
				redisClient.LPush(ctx, queue.AlertsPoisonQueueKey, poisonBytes)
				poisoned++
				continue
			}
			_ = queue.PushToDLQ(ctx, redisClient, entry)
			requeued++
		}

		remaining, _ := redisClient.LLen(ctx, queue.AlertsDLQQueueKey).Result()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DLQReplayResponse{
			Replayed:       replayed,
			Requeued:       requeued,
			Poisoned:       poisoned,
			RemainingInDLQ: int(remaining),
		})
	}
}
