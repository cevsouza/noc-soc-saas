package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"noc-api/internal/cache"
	"noc-api/internal/connector"
	"noc-api/internal/db"
	"noc-api/internal/model"
	"noc-api/internal/queue"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// snsSubscribeURLHost matches AWS SNS's actual SubscribeURL host pattern (e.g.
// sns.us-east-1.amazonaws.com). Only URLs whose host matches this are ever fetched — a forged
// SubscriptionConfirmation body naming an attacker-controlled SubscribeURL is rejected before
// any outbound request is made (SSRF guard).
var snsSubscribeURLHost = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com$`)

var snsConfirmClient = &http.Client{Timeout: 5 * time.Second}

// snsEnvelope is AWS SNS's HTTP(S) subscription delivery envelope. Real CloudWatch alarm data
// arrives as the *string-encoded* JSON in Message when Type == "Notification" — it is never
// parsed here; it's handed off byte-for-byte to the cloudwatchConnector (see
// internal/connector/cloudwatch.go), keeping MapToUnified a pure function.
type snsEnvelope struct {
	Type         string `json:"Type"`
	MessageID    string `json:"MessageId"`
	TopicArn     string `json:"TopicArn"`
	Subject      string `json:"Subject"`
	Message      string `json:"Message"`
	SubscribeURL string `json:"SubscribeURL"`
	Timestamp    string `json:"Timestamp"`
}

// HandleCloudWatchIngest ingests CloudWatch alarms delivered via an SNS HTTP(S) subscription.
// Unlike the other ingest handlers, it must also handle SNS's subscription handshake: the
// first delivery to a new subscription is a SubscriptionConfirmation carrying a SubscribeURL
// that must be fetched (GET) to actually activate the subscription before any real
// Notification messages will be delivered.
func HandleCloudWatchIngest(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var exists bool
		err := pgPool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')", tenantID, "cloudwatch").Scan(&exists)
		if err != nil || !exists {
			http.Error(w, "Forbidden: CloudWatch integration not active for this tenant", http.StatusForbidden)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Bad Request: failed to read body", http.StatusBadRequest)
			return
		}

		var envelope snsEnvelope
		if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
			http.Error(w, "Bad Request: invalid SNS envelope JSON", http.StatusBadRequest)
			return
		}

		switch envelope.Type {
		case "SubscriptionConfirmation":
			handleSNSSubscriptionConfirmation(w, tenantID, envelope)
			return
		case "UnsubscribeConfirmation":
			w.WriteHeader(http.StatusOK)
			return
		case "Notification":
			// fall through to alarm processing below
		default:
			http.Error(w, "Bad Request: unknown SNS message Type", http.StatusBadRequest)
			return
		}

		incidents, err := connector.MustGet(model.SourceCloudWatch).MapToUnified([]byte(envelope.Message), tenantID)
		if err != nil {
			errMsg := fmt.Sprintf("Bad Request: Invalid CloudWatch alarm JSON payload: %v", err)
			redisClient.Set(r.Context(), cache.TenantKey(tenantID, "webhook_error","cloudwatch"), errMsg, 24*time.Hour)
			http.Error(w, "Bad Request: Invalid CloudWatch alarm JSON payload", http.StatusBadRequest)
			return
		}
		incident := incidents[0]

		eventBytes, err := json.Marshal(incident)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if err := redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, eventBytes).Err(); err != nil {
			http.Error(w, "Internal Server Error: Failed to queue CloudWatch alert", http.StatusInternalServerError)
			return
		}

		redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat","cloudwatch"), time.Now().Unix(), 24*time.Hour)
		redisClient.Del(r.Context(), cache.TenantKey(tenantID, "webhook_error","cloudwatch"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(IngestResponse{
			Status:    "accepted",
			ID:        incident.ID,
			Message:   "CloudWatch alarm successfully normalized and queued",
			Timestamp: incident.Timestamp,
		})
	}
}

// isValidSNSSubscribeURL reports whether rawURL is safe to fetch: must be https:// and its
// host must match AWS SNS's real SubscribeURL host pattern. Kept as a pure, dependency-free
// predicate so it's unit-testable without any network I/O.
func isValidSNSSubscribeURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && snsSubscribeURLHost.MatchString(parsed.Host)
}

func handleSNSSubscriptionConfirmation(w http.ResponseWriter, tenantID uuid.UUID, envelope snsEnvelope) {
	if !isValidSNSSubscribeURL(envelope.SubscribeURL) {
		log.Printf("[CloudWatch SNS] Rejected SubscribeURL for tenant %s: failed host/scheme validation (%q)", tenantID, envelope.SubscribeURL)
		http.Error(w, "Bad Request: SubscribeURL failed validation", http.StatusBadRequest)
		return
	}

	resp, err := snsConfirmClient.Get(envelope.SubscribeURL)
	if err != nil {
		http.Error(w, "Internal Server Error: failed to confirm SNS subscription", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Internal Server Error: SNS confirmation returned status %d", resp.StatusCode), http.StatusInternalServerError)
		return
	}

	log.Printf("[CloudWatch SNS] Subscription confirmed for tenant %s (topic %s)", tenantID, envelope.TopicArn)
	w.WriteHeader(http.StatusOK)
}
