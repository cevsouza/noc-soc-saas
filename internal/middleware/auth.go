package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	ApiKeyHeader     = "X-API-Key"
	ApiKeyCachePrefix = "noc:cache:apikey:"
	ApiKeyCacheTTL    = 5 * time.Minute
)

// APIKeyAuth is a high-performance middleware that authenticates requests using an API Key.
// It uses Redis caching to avoid hitting PostgreSQL on every single event ingestion request under high load.
// It also falls back to resolving the "token" query parameter for simple webhooks (supporting raw UUID, JWT, or API Keys).
func APIKeyAuth(pgPool *pgxpool.Pool, redisClient *redis.Client, jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get(ApiKeyHeader)
			
			var tenantID uuid.UUID
			var err error

			if apiKey != "" {
				// 1. Authenticate using API Key header (standard high-performance flow with Redis cache)
				hash := sha256.Sum256([]byte(apiKey))
				keyHash := hex.EncodeToString(hash[:])

				ctx := r.Context()
				cacheKey := ApiKeyCachePrefix + keyHash
				cachedTenantID, err := redisClient.Get(ctx, cacheKey).Result()
				if err == nil && cachedTenantID != "" {
					tenantID, err = uuid.Parse(cachedTenantID)
					if err == nil {
						// Cache hit! Inject tenant ID and proceed
						ctx = db.WithTenantID(ctx, tenantID)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
				}

				// Cache miss: Query PostgreSQL to resolve tenant ID
				query := `
					SELECT tenant_id 
					FROM tenant_api_keys 
					WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > NOW())
				`
				err = pgPool.QueryRow(ctx, query, keyHash).Scan(&tenantID)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						http.Error(w, "Unauthorized: Invalid or expired API Key", http.StatusUnauthorized)
						return
					}
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}

				// Cache the successful resolution in Redis
				_ = redisClient.Set(ctx, cacheKey, tenantID.String(), ApiKeyCacheTTL).Err()

				// Inject tenant ID into context and proceed
				ctx = db.WithTenantID(ctx, tenantID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 2. Fallback to query parameter "token" (standard for simple webhooks)
			token := r.URL.Query().Get("token")
			if token != "" {
				tenantID, err = ResolveTenantFromToken(token, jwtSecret, pgPool)
				if err == nil {
					ctx := db.WithTenantID(r.Context(), tenantID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			http.Error(w, "Unauthorized: Missing API Key header or valid token parameter", http.StatusUnauthorized)
		})
	}
}

type contextKey string

const ClaimsContextKey contextKey = "jwt_claims"

// JWTClaims maps the standard claims needed for NOC/SOC multi-tenant operations.
// Role is scoped to TenantID (the role the user holds in that specific tenant).
// GlobalRole is platform-wide (MSP-level) and is the only claim that may authorize
// cross-tenant actions such as tenant_id=all or acting on a tenant_id other than TenantID.
type JWTClaims struct {
	UserID     uuid.UUID      `json:"user_id"`
	TenantID   uuid.UUID      `json:"tenant_id"`
	Role       model.UserRole `json:"role"`
	GlobalRole model.UserRole `json:"global_role"`
	Email      string         `json:"email"`
	Exp        int64          `json:"exp"`
}

// JWTAuth verifica tokens JWT. Para testes temporários (omissão de autenticação), injeta dados de administrador se o token for ausente/inválido.
func JWTAuth(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			var claims *JWTClaims
			var err error

			if authHeader != "" && len(authHeader) >= 8 && authHeader[:7] == "Bearer " {
				tokenString := authHeader[7:]
				claims, err = VerifyJWT(tokenString, jwtSecret)
				if err == nil {
					// Valida expiração
					if time.Now().Unix() > claims.Exp {
						err = errors.New("token expired")
					}
				}
			} else {
				err = errors.New("missing token")
			}

			if err != nil {
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// Injeta tenant ID e claims no contexto
			ctx := r.Context()
			ctx = db.WithTenantID(ctx, claims.TenantID)
			ctx = context.WithValue(ctx, ClaimsContextKey, claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext retrieves JWT claims from context.
func ClaimsFromContext(ctx context.Context) (*JWTClaims, bool) {
	claims, ok := ctx.Value(ClaimsContextKey).(*JWTClaims)
	return claims, ok
}

// RequireRole checks if the authenticated user has one of the allowed roles.
func RequireRole(allowedRoles ...model.UserRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized: User context missing", http.StatusUnauthorized)
				return
			}

			roleAllowed := false
			for _, allowed := range allowedRoles {
				if claims.Role == allowed {
					roleAllowed = true
					break
				}
			}

			if !roleAllowed {
				http.Error(w, "Forbidden: Insufficient privileges", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireGlobalRole checks the platform-wide GlobalRole claim rather than the tenant-scoped
// Role claim. Use this — never RequireRole — to gate actions that are not scoped to the
// caller's own tenant (e.g. creating/deleting tenants), since any tenant's own admin has
// Role==admin but that must not authorize acting on other tenants.
func RequireGlobalRole(allowedRoles ...model.UserRole) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized: User context missing", http.StatusUnauthorized)
				return
			}

			roleAllowed := false
			for _, allowed := range allowedRoles {
				if claims.GlobalRole == allowed {
					roleAllowed = true
					break
				}
			}

			if !roleAllowed {
				http.Error(w, "Forbidden: platform admin privileges required", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// VerifyJWT validates an HMAC-SHA256 JWT signature and returns decrypted claims.
func VerifyJWT(tokenString string, secret []byte) (*JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	headerSegment := parts[0]
	payloadSegment := parts[1]
	signatureSegment := parts[2]

	// Verify signature
	signingInput := headerSegment + "." + payloadSegment
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSignature := mac.Sum(nil)

	sigBytes, err := base64.RawURLEncoding.DecodeString(signatureSegment)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %w", err)
	}

	if !hmac.Equal(sigBytes, expectedSignature) {
		return nil, errors.New("invalid signature")
	}

	// Decode payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %w", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claims: %w", err)
	}

	return &claims, nil
}

// GenerateJWT creates a new signed JWT for testing or API usage.
func GenerateJWT(claims *JWTClaims, secret []byte) (string, error) {
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", err
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	headerSegment := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadSegment := base64.RawURLEncoding.EncodeToString(payloadBytes)

	signingInput := headerSegment + "." + payloadSegment
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	signature := mac.Sum(nil)
	signatureSegment := base64.RawURLEncoding.EncodeToString(signature)

	return headerSegment + "." + payloadSegment + "." + signatureSegment, nil
}

// ResolveTenantFromToken resolves a token parameter into a tenant ID.
// SECURITY: this must NEVER accept a raw UUID as a valid credential — a tenant ID is not a
// secret (it is exposed publicly via /api/v1/public/tenants), so only a signed JWT or a real
// API key hash may resolve a tenant identity here.
func ResolveTenantFromToken(token string, jwtSecret []byte, pgPool *pgxpool.Pool) (uuid.UUID, error) {
	if token == "" {
		return uuid.Nil, errors.New("empty token")
	}

	// 1. Try verifying as signed JWT token
	claims, err := VerifyJWT(token, jwtSecret)
	if err == nil {
		// Verify token expiry
		if time.Now().Unix() > claims.Exp {
			return uuid.Nil, errors.New("token expired")
		}
		return claims.TenantID, nil
	}

	// 2. Try resolving as API Key hash
	ctx := context.Background()
	hash := sha256.Sum256([]byte(token))
	keyHash := hex.EncodeToString(hash[:])
	var tenantID uuid.UUID
	query := `
		SELECT tenant_id 
		FROM tenant_api_keys 
		WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > NOW())
	`
	err = pgPool.QueryRow(ctx, query, keyHash).Scan(&tenantID)
	if err == nil {
		return tenantID, nil
	}

	return uuid.Nil, errors.New("invalid token")
}

// RateLimiter protects endpoints from DOS/brute-force by applying a per-tenant limit on Redis.
func RateLimiter(redisClient *redis.Client, limit int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID, ok := db.TenantIDFromContext(r.Context())
			if !ok {
				// If no tenant is resolved yet, skip rate limiting
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			currentMinute := time.Now().Unix() / 60
			key := fmt.Sprintf("ratelimit:tenant:%s:%d", tenantID.String(), currentMinute)

			// Multi/Pipeline increments and sets TTL of 60s
			pipe := redisClient.Pipeline()
			incr := pipe.Incr(ctx, key)
			pipe.Expire(ctx, key, 60*time.Second)

			_, err := pipe.Exec(ctx)
			if err != nil {
				// Fails open if Redis has issues (avoid blocking critical telemetry)
				next.ServeHTTP(w, r)
				return
			}

			if incr.Val() > int64(limit) {
				http.Error(w, "Too Many Requests: Rate limit exceeded for this tenant (Max 500/min)", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

