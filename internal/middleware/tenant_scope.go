package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ScopeError carries an HTTP status code alongside a safe-to-display message,
// so handlers can surface the correct status without leaking internals.
type ScopeError struct {
	Code int
	Msg  string
}

func (e *ScopeError) Error() string { return e.Msg }

// ResolveTenantScope is the ONLY authorized way to decide which tenant a request operates on.
// It always requires valid JWT claims in the request context. A request may only act on a
// tenant other than the caller's own (claims.TenantID) if the caller is a platform-wide admin
// (claims.GlobalRole == admin) or is an explicit member of that tenant (tenant_users).
//
// This replaces the old resolveTenantID() helper, which trusted any `?tenant_id=` query
// parameter unconditionally — since tenant IDs are not secret, that allowed any authenticated
// user to read or act on any other tenant's data.
func ResolveTenantScope(ctx context.Context, r *http.Request, pgPool *pgxpool.Pool) (uuid.UUID, error) {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return uuid.Nil, &ScopeError{http.StatusUnauthorized, "Unauthorized: missing authentication"}
	}

	requested := r.URL.Query().Get("tenant_id")
	if requested == "" || requested == claims.TenantID.String() {
		return claims.TenantID, nil
	}

	requestedID, err := uuid.Parse(requested)
	if err != nil {
		return uuid.Nil, &ScopeError{http.StatusBadRequest, "Bad Request: invalid tenant_id"}
	}

	if model.IsPlatformAdmin(claims.GlobalRole) {
		var exists bool
		if err := pgPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)", requestedID).Scan(&exists); err != nil || !exists {
			return uuid.Nil, &ScopeError{http.StatusNotFound, "Not Found: tenant does not exist"}
		}
		return requestedID, nil
	}

	member, err := isTenantMember(ctx, pgPool, claims.UserID, requestedID)
	if err != nil {
		return uuid.Nil, &ScopeError{http.StatusInternalServerError, "Internal Server Error: failed to verify tenant membership"}
	}
	if !member {
		return uuid.Nil, &ScopeError{http.StatusForbidden, "Forbidden: not a member of the requested tenant"}
	}
	return requestedID, nil
}

func isTenantMember(ctx context.Context, pgPool *pgxpool.Pool, userID, tenantID uuid.UUID) (bool, error) {
	var exists bool
	err := pgPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_users WHERE user_id = $1 AND tenant_id = $2)", userID, tenantID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// AllTenantsScope reports whether the request explicitly asked for the special
// tenant_id=all mode, which is exclusive to platform-wide admins (claims.GlobalRole == admin).
// Callers that honor this must run their query with `SET LOCAL app.bypass_rls = 'true'`.
func AllTenantsScope(ctx context.Context, r *http.Request) (bool, error) {
	if r.URL.Query().Get("tenant_id") != "all" {
		return false, nil
	}
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return false, &ScopeError{http.StatusUnauthorized, "Unauthorized: missing authentication"}
	}
	if !model.IsPlatformAdmin(claims.GlobalRole) {
		return false, &ScopeError{http.StatusForbidden, "Forbidden: platform admin required for tenant_id=all"}
	}
	return true, nil
}

// WriteScopeError writes a ScopeError (or any other error) as an HTTP response with the
// correct status code, defaulting to 401 for errors that aren't a *ScopeError.
func WriteScopeError(w http.ResponseWriter, err error) {
	var se *ScopeError
	if errors.As(err, &se) {
		http.Error(w, se.Msg, se.Code)
		return
	}
	http.Error(w, fmt.Sprintf("Unauthorized: %v", err), http.StatusUnauthorized)
}
