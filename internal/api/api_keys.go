package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Self-service ingestion API keys (in-app "Como conectar" onboarding). Until now a tenant could only
// obtain an ingest credential by enrolling an agent or by touching the database — undiscoverable for
// a real customer. These endpoints let a tenant admin mint, list and revoke keys from the UI so the
// connection procedure is fully self-service.
//
// Keys are stored only as a SHA-256 hash (tenant_api_keys.key_hash); the plaintext is returned once,
// on creation, and never again. All queries run inside the tenant RLS context AND filter tenant_id
// explicitly (defense in depth). Revocation also busts the Redis auth cache so it takes effect at once.

// APIKeyInfo is a key as listed (never includes the secret).
type APIKeyInfo struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreatedAPIKey is the create response — the only time the plaintext key is exposed.
type CreatedAPIKey struct {
	APIKeyInfo
	APIKey string `json:"api_key"`
}

// HandleListAPIKeys returns the tenant's ingestion keys (names/dates only, never the secret).
func HandleListAPIKeys(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		keys := []APIKeyInfo{}
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `SELECT id, name, created_at, expires_at FROM tenant_api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var k APIKeyInfo
				if e := rows.Scan(&k.ID, &k.Name, &k.CreatedAt, &k.ExpiresAt); e != nil {
					return e
				}
				keys = append(keys, k)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to list API keys", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": keys})
	}
}

// HandleCreateAPIKey mints a new ingestion key for the tenant and returns the plaintext once.
func HandleCreateAPIKey(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name := body.Name
		if name == "" {
			name = "Chave de ingestão"
		}
		if len(name) > 120 {
			http.Error(w, "Bad Request: name too long", http.StatusBadRequest)
			return
		}

		// noc_ prefix makes the credential recognizable; the stored hash covers the full string.
		raw := make([]byte, 24)
		if _, e := rand.Read(raw); e != nil {
			http.Error(w, "Failed to generate key", http.StatusInternalServerError)
			return
		}
		plain := "noc_" + hex.EncodeToString(raw)
		sum := sha256.Sum256([]byte(plain))
		keyHash := hex.EncodeToString(sum[:])

		ctx := db.WithTenantID(r.Context(), tenantID)
		var created CreatedAPIKey
		created.Name = name
		created.APIKey = plain
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `INSERT INTO tenant_api_keys (tenant_id, name, key_hash) VALUES ($1, $2, $3) RETURNING id, created_at`,
				tenantID, name, keyHash).Scan(&created.ID, &created.CreatedAt)
		})
		if err != nil {
			http.Error(w, "Failed to create API key", http.StatusInternalServerError)
			return
		}

		audit.Record(r.Context(), pgPool, audit.Entry{
			TenantID: tenantID, UserID: actorID(r),
			Action: "apikey.create", Resource: created.ID.String(),
			Details: map[string]interface{}{"name": name}, IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(created)
	}
}

// HandleRevokeAPIKey deletes a key by id (?id=) and busts its auth cache so it stops working at once.
func HandleRevokeAPIKey(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		keyID, err := uuid.Parse(r.URL.Query().Get("id"))
		if err != nil {
			http.Error(w, "Bad Request: id must be a valid UUID", http.StatusBadRequest)
			return
		}

		ctx := db.WithTenantID(r.Context(), tenantID)
		var keyHash string
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// Capture the hash (tenant-scoped) so we can bust the cache, then delete the row.
			e := tx.QueryRow(ctx, `SELECT key_hash FROM tenant_api_keys WHERE id = $1 AND tenant_id = $2`, keyID, tenantID).Scan(&keyHash)
			if e != nil {
				return e
			}
			_, e = tx.Exec(ctx, `DELETE FROM tenant_api_keys WHERE id = $1 AND tenant_id = $2`, keyID, tenantID)
			return e
		})
		if err != nil {
			if err == pgx.ErrNoRows {
				http.Error(w, "Not Found: API key not found", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to revoke API key", http.StatusInternalServerError)
			return
		}
		// Bust the Redis auth cache (best-effort) so the revoked key can't authenticate via a warm cache.
		if keyHash != "" {
			_ = redisClient.Del(r.Context(), middleware.ApiKeyCachePrefix+keyHash).Err()
		}

		audit.Record(r.Context(), pgPool, audit.Entry{
			TenantID: tenantID, UserID: actorID(r),
			Action: "apikey.revoke", Resource: keyID.String(), IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"revoked": true})
	}
}

// actorID pulls the acting user's id from the JWT claims for audit, or the zero UUID if absent.
func actorID(r *http.Request) uuid.UUID {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		return claims.UserID
	}
	return uuid.Nil
}
