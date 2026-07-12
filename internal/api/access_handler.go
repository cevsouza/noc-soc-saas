package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantAccessGrant is one row of a user's authorized-tenant list: which tenant, its display
// name, and the role the user holds there (from tenant_users).
type TenantAccessGrant struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	TenantName string    `json:"tenant_name"`
	Role       string    `json:"role"`
}

// GrantAccessRequest is the body of POST /api/v1/admin/access. Role is intentionally NOT
// accepted from the client in this slice — every grant is a plain 'operator' membership
// (see the plan's Fase 5 fatia 1 decision). The field is kept out so a caller cannot smuggle
// in an 'admin' grant through this endpoint.
type GrantAccessRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

// The single role every grant created through this endpoint receives. Per-tenant role
// granularity is a deliberate future extension, not part of this slice.
const grantedTenantRole = "operator"

// parseGrantAccessRequest validates and parses a GrantAccessRequest into concrete UUIDs.
// Split out from the handler so it can be unit-tested without a database (mirrors the
// resolveSearchTenantIDs pattern in search_handler.go).
func parseGrantAccessRequest(req GrantAccessRequest) (userID, tenantID uuid.UUID, err error) {
	userID, err = uuid.Parse(req.UserID)
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("invalid or missing user_id")
	}
	tenantID, err = uuid.Parse(req.TenantID)
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("invalid or missing tenant_id")
	}
	return userID, tenantID, nil
}

// HandleGetUserAccess lists the tenants a given user is authorized on. Admin-global-only
// (gated at the route in main.go via RequireGlobalRole).
//
// GET /api/v1/admin/access?user_id=<uuid>
func HandleGetUserAccess(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := uuid.Parse(r.URL.Query().Get("user_id"))
		if err != nil {
			http.Error(w, "Bad Request: invalid or missing user_id", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		rows, err := pgPool.Query(ctx, `
			SELECT tu.tenant_id, t.name, tu.role
			FROM tenant_users tu
			JOIN tenants t ON t.id = tu.tenant_id
			WHERE tu.user_id = $1
			ORDER BY t.name
		`, userID)
		if err != nil {
			http.Error(w, "Internal Server Error: failed to query access grants", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		grants := make([]TenantAccessGrant, 0)
		for rows.Next() {
			var g TenantAccessGrant
			if err := rows.Scan(&g.TenantID, &g.TenantName, &g.Role); err != nil {
				http.Error(w, "Internal Server Error: failed to scan access grant", http.StatusInternalServerError)
				return
			}
			grants = append(grants, g)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(grants)
	}
}

// HandleGrantUserAccess grants a user 'operator' access to a specific tenant. Idempotent:
// re-granting an existing membership just re-asserts the operator role. Admin-global-only.
//
// POST /api/v1/admin/access   body: {user_id, tenant_id}
func HandleGrantUserAccess(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req GrantAccessRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: invalid JSON payload", http.StatusBadRequest)
			return
		}

		userID, tenantID, err := parseGrantAccessRequest(req)
		if err != nil {
			http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		// Validate that both the user and the tenant actually exist, so we return a clean 404
		// instead of a foreign-key error leaking from the database.
		var userExists, tenantExists bool
		if err := pgPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)", userID).Scan(&userExists); err != nil {
			http.Error(w, "Internal Server Error: failed to verify user", http.StatusInternalServerError)
			return
		}
		if !userExists {
			http.Error(w, "Not Found: user does not exist", http.StatusNotFound)
			return
		}
		if err := pgPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)", tenantID).Scan(&tenantExists); err != nil {
			http.Error(w, "Internal Server Error: failed to verify tenant", http.StatusInternalServerError)
			return
		}
		if !tenantExists {
			http.Error(w, "Not Found: tenant does not exist", http.StatusNotFound)
			return
		}

		_, err = pgPool.Exec(ctx, `
			INSERT INTO tenant_users (tenant_id, user_id, role, created_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (tenant_id, user_id) DO UPDATE SET role = EXCLUDED.role
		`, tenantID, userID, grantedTenantRole)
		if err != nil {
			http.Error(w, "Internal Server Error: failed to grant access", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","message":"Acesso concedido"}`))
	}
}

// HandleRevokeUserAccess removes a user's access to a specific tenant. Admin-global-only.
//
// Because JWTs are stateless (24h), revocation takes effect immediately for any tenant-scoped
// request routed through ResolveTenantScope/isTenantMember (those re-check tenant_users on every
// request), while the user's "home" claims.TenantID baked into an already-issued token survives
// until it expires or the user logs in again.
//
// DELETE /api/v1/admin/access?user_id=<uuid>&tenant_id=<uuid>
func HandleRevokeUserAccess(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := uuid.Parse(r.URL.Query().Get("user_id"))
		if err != nil {
			http.Error(w, "Bad Request: invalid or missing user_id", http.StatusBadRequest)
			return
		}
		tenantID, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
		if err != nil {
			http.Error(w, "Bad Request: invalid or missing tenant_id", http.StatusBadRequest)
			return
		}

		// Anti-lockout: don't let an admin revoke their own membership through this screen — the
		// same spirit as HandleDeleteUser refusing to delete the caller's own session user.
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok && claims.UserID == userID {
			http.Error(w, "Conflict: cannot revoke your own tenant access here", http.StatusConflict)
			return
		}

		ctx := r.Context()
		if _, err := pgPool.Exec(ctx, "DELETE FROM tenant_users WHERE user_id = $1 AND tenant_id = $2", userID, tenantID); err != nil {
			http.Error(w, "Internal Server Error: failed to revoke access", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","message":"Acesso revogado"}`))
	}
}
