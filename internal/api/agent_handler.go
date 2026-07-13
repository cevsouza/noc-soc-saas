package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/cache"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"noc-api/internal/queue"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// The NOC/SOC agent talks to the SaaS only outbound over 443: it enrolls once (token -> API key),
// polls its config, and pushes heartbeats + events. See migration 000023 for the data model.

// randToken returns a 32-byte random secret as hex, plus its sha256 hash (stored at rest).
func randToken() (plain, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	plain = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(plain))
	return plain, hex.EncodeToString(sum[:]), nil
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func agentEnrollmentTTL() time.Duration {
	if v := os.Getenv("AGENT_ENROLLMENT_TTL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return 60 * time.Minute
}

// HandleCreateEnrollmentToken mints a one-time enrollment token for the tenant (route-gated to
// tenant admins). The plaintext is returned exactly once; only its hash is stored.
func HandleCreateEnrollmentToken(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		plain, hash, err := randToken()
		if err != nil {
			http.Error(w, "Failed to generate token", http.StatusInternalServerError)
			return
		}
		expiresAt := time.Now().Add(agentEnrollmentTTL())

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO agent_enrollment_tokens (tenant_id, token_hash, expires_at) VALUES ($1, $2, $3)`, tenantID, hash, expiresAt)
			return e
		})
		if err != nil {
			http.Error(w, "Failed to store enrollment token", http.StatusInternalServerError)
			return
		}

		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}
		audit.Record(ctx, pgPool, audit.Entry{
			TenantID: tenantID, UserID: actorID,
			Action: "agent.enroll_token.create", Resource: tenantID.String(), IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"enrollment_token": plain,
			"expires_at":       expiresAt,
		})
	}
}

// EnrollRequest is the agent's enrollment payload.
type EnrollRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	Hostname        string `json:"hostname"`
	Version         string `json:"version"`
}

// HandleEnrollAgent exchanges a valid one-time enrollment token for a tenant API key. Unauthenticated
// at the route (the token IS the credential). The cross-tenant token lookup uses the privileged pool
// by its globally-unique hash (same pattern as login / API-key auth); the writes then run inside the
// token's tenant RLS context.
func HandleEnrollAgent(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req EnrollRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EnrollmentToken == "" {
			http.Error(w, "Bad Request: enrollment_token is required", http.StatusBadRequest)
			return
		}
		if req.Hostname == "" {
			req.Hostname = "unknown-host"
		}

		ctx := r.Context()
		hash := hashToken(req.EnrollmentToken)

		var tokenID, tenantID uuid.UUID
		var expiresAt time.Time
		var usedAt *time.Time
		err := pgPool.QueryRow(ctx, `SELECT id, tenant_id, expires_at, used_at FROM agent_enrollment_tokens WHERE token_hash = $1`, hash).
			Scan(&tokenID, &tenantID, &expiresAt, &usedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Unauthorized: invalid enrollment token", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if usedAt != nil {
			http.Error(w, "Conflict: enrollment token already used", http.StatusConflict)
			return
		}
		if time.Now().After(expiresAt) {
			http.Error(w, "Unauthorized: enrollment token expired", http.StatusUnauthorized)
			return
		}

		apiKeyPlain, apiKeyHash, err := randToken()
		if err != nil {
			http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
			return
		}

		tctx := db.WithTenantID(ctx, tenantID)
		var agentID uuid.UUID
		err = db.ExecuteInTenantTx(tctx, pgPool, func(tx pgx.Tx) error {
			// Claim the token atomically (one-time use): only proceed if we flip used_at.
			tag, e := tx.Exec(tctx, `UPDATE agent_enrollment_tokens SET used_at = NOW() WHERE id = $1 AND used_at IS NULL`, tokenID)
			if e != nil {
				return e
			}
			if tag.RowsAffected() == 0 {
				return errTokenRace
			}
			var apiKeyID uuid.UUID
			if e := tx.QueryRow(tctx, `INSERT INTO tenant_api_keys (tenant_id, name, key_hash) VALUES ($1, $2, $3) RETURNING id`,
				tenantID, "agent:"+req.Hostname, apiKeyHash).Scan(&apiKeyID); e != nil {
				return e
			}
			return tx.QueryRow(tctx, `INSERT INTO agents (tenant_id, name, api_key_id, version, last_seen) VALUES ($1, $2, $3, $4, NOW()) RETURNING id`,
				tenantID, req.Hostname, apiKeyID, req.Version).Scan(&agentID)
		})
		if errors.Is(err, errTokenRace) {
			http.Error(w, "Conflict: enrollment token already used", http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, "Failed to enroll agent", http.StatusInternalServerError)
			return
		}

		audit.Record(tctx, pgPool, audit.Entry{
			TenantID: tenantID, Action: "agent.enroll", Resource: agentID.String(),
			Details: map[string]interface{}{"hostname": req.Hostname, "version": req.Version}, IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"agent_id": agentID,
			"api_key":  apiKeyPlain,
		})
	}
}

var errTokenRace = errors.New("enrollment token already claimed")

// AgentConfig is what the agent polls to learn what to do — including its per-tenant SNMP targets
// (slice 2), with the community decrypted for the authenticated agent.
type AgentConfig struct {
	HeartbeatIntervalSeconds int               `json:"heartbeat_interval_seconds"`
	PollIntervalSeconds      int               `json:"poll_interval_seconds"`
	SNMPTargets              []AgentSNMPTarget `json:"snmp_targets"`
}

// HandleGetAgentConfig returns the agent's configuration (API-key auth). Optional ?agent_id refreshes
// that agent's last_seen.
func HandleGetAgentConfig(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		touchAgent(r, pgPool, tenantID)

		targets := make([]AgentSNMPTarget, 0)
		if masterKey, err := security.GetMasterKey(); err == nil {
			ctx := db.WithTenantID(r.Context(), tenantID)
			_ = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				t, e := loadAgentSNMPTargets(ctx, tx, tenantID, masterKey)
				if e == nil {
					targets = t
				}
				return e
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AgentConfig{
			HeartbeatIntervalSeconds: 60,
			PollIntervalSeconds:      60,
			SNMPTargets:              targets,
		})
	}
}

// AgentEvent is one event pushed by the agent (maps onto the internal UnifiedIncident).
type AgentEvent struct {
	Source      string `json:"source"`
	ExternalID  string `json:"external_id"`
	EventType   string `json:"event_type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Host        string `json:"host"`
}

// AgentEventsRequest is the agent's push batch. An empty events array is a valid heartbeat-only ping.
type AgentEventsRequest struct {
	AgentID uuid.UUID    `json:"agent_id"`
	Events  []AgentEvent `json:"events"`
}

// HandleAgentEvents ingests a batch of agent events into the normal pipeline and refreshes the
// agent's liveness (last_seen + heartbeat cache). API-key auth.
func HandleAgentEvents(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var req AgentEventsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: invalid payload", http.StatusBadRequest)
			return
		}

		accepted := 0
		for _, ev := range req.Events {
			source := model.IncidentSource(ev.Source)
			if source == "" {
				source = "agent"
			}
			sev := model.AlertSeverity(ev.Severity)
			if _, valid := map[model.AlertSeverity]bool{model.SeverityInfo: true, model.SeverityWarning: true, model.SeverityCritical: true, model.SeverityFatal: true}[sev]; !valid {
				sev = model.SeverityWarning
			}
			inc := model.UnifiedIncident{
				ID:          uuid.New(),
				TenantID:    tenantID,
				Source:      source,
				ExternalID:  ev.ExternalID,
				EventType:   ev.EventType,
				Severity:    sev,
				Title:       ev.Title,
				Description: ev.Description,
				Host:        ev.Host,
				Timestamp:   time.Now(),
			}
			b, err := json.Marshal(inc)
			if err != nil {
				continue
			}
			if err := redisClient.LPush(r.Context(), queue.AlertsNormalizedQueueKey, b).Err(); err != nil {
				http.Error(w, "Failed to enqueue events", http.StatusInternalServerError)
				return
			}
			accepted++
		}

		// Liveness: refresh last_seen + the heartbeat cache the watchdog/console read.
		touchAgent(r, pgPool, tenantID)
		_ = redisClient.Set(r.Context(), cache.TenantKey(tenantID, "heartbeat", "agent"), time.Now().Unix(), 24*time.Hour).Err()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"accepted": accepted})
	}
}

// touchAgent refreshes an agent's last_seen when the request carries a valid ?agent_id for the tenant.
func touchAgent(r *http.Request, pgPool *pgxpool.Pool, tenantID uuid.UUID) {
	agentID, err := uuid.Parse(r.URL.Query().Get("agent_id"))
	if err != nil {
		return
	}
	ctx := db.WithTenantID(r.Context(), tenantID)
	_ = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE agents SET last_seen = NOW() WHERE id = $1 AND tenant_id = $2`, agentID, tenantID)
		return e
	})
}
