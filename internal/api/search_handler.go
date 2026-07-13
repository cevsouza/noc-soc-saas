package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/middleware"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SearchAlertResult struct {
	ID        uuid.UUID `json:"id"`
	Summary   string    `json:"summary"`
	Severity  string    `json:"severity"`
	TenantID  uuid.UUID `json:"tenant_id"`
	CreatedAt time.Time `json:"created_at"`
}

type SearchRunbookResult struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	TenantID uuid.UUID `json:"tenant_id"`
	IsGlobal bool      `json:"is_global"`
}

type SearchTenantResult struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type GlobalSearchResponse struct {
	Alerts   []SearchAlertResult   `json:"alerts"`
	Runbooks []SearchRunbookResult `json:"runbooks"`
	Tenants  []SearchTenantResult  `json:"tenants"`
}

// resolveSearchTenantIDs validates the comma-separated `tenants` query param against the
// caller's actual access, mirroring internal/ws/ws_handler.go's ServeWS tenant-list validation
// (a UUID is accepted if it matches the caller's own tenant or the caller is a platform-wide
// admin, otherwise real tenant_users membership is required) — unauthorized IDs are silently
// dropped rather than erroring, since the caller may legitimately have access to only some of
// the tenants it asked about (e.g. a stale tenant selector). If tenantsParam is empty, the
// caller's own tenant is used. Kept as its own copy rather than extracted into
// internal/middleware/tenant_scope.go, consistent with this codebase's existing convention of
// a separate isTenantMember/isWSTenantMember copy per caller rather than a shared helper.
func resolveSearchTenantIDs(claims *middleware.JWTClaims, tenantsParam string, isMember func(tenantID uuid.UUID) bool) []uuid.UUID {
	if tenantsParam == "" {
		return []uuid.UUID{claims.TenantID}
	}

	var tenantIDs []uuid.UUID
	for _, tok := range strings.Split(tenantsParam, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		parsed, err := uuid.Parse(tok)
		if err != nil {
			continue
		}
		if parsed == claims.TenantID || model.IsPlatformAdmin(claims.GlobalRole) {
			tenantIDs = append(tenantIDs, parsed)
			continue
		}
		if isMember(parsed) {
			tenantIDs = append(tenantIDs, parsed)
		} else {
			log.Printf("[Search Security] User %s denied search scope for unauthorized tenant %s", claims.UserID, parsed)
		}
	}
	return tenantIDs
}

// HandleGlobalSearch searches alerts, runbooks, and tenants matching ?q= within the tenants
// the caller has access to (?tenants=, comma-separated — defaults to the caller's own tenant).
func HandleGlobalSearch(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: User claims missing", http.StatusUnauthorized)
			return
		}

		query := strings.TrimSpace(r.URL.Query().Get("q"))
		resp := GlobalSearchResponse{
			Alerts:   []SearchAlertResult{},
			Runbooks: []SearchRunbookResult{},
			Tenants:  []SearchTenantResult{},
		}
		if len(query) < 2 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		ctx := r.Context()
		tenantIDs := resolveSearchTenantIDs(claims, r.URL.Query().Get("tenants"), func(tenantID uuid.UUID) bool {
			var exists bool
			_ = pgPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_users WHERE user_id = $1 AND tenant_id = $2)", claims.UserID, tenantID).Scan(&exists)
			return exists
		})
		if len(tenantIDs) == 0 {
			http.Error(w, "Forbidden: not authorized for any of the requested tenants", http.StatusForbidden)
			return
		}

		likeTerm := "%" + query + "%"

		alertRows, err := pgPool.Query(ctx, `
			SELECT id, summary, severity, tenant_id, created_at
			FROM alerts
			WHERE tenant_id = ANY($1) AND (summary ILIKE $2 OR event_type ILIKE $2)
			ORDER BY created_at DESC
			LIMIT 8
		`, tenantIDs, likeTerm)
		if err == nil {
			defer alertRows.Close()
			for alertRows.Next() {
				var a SearchAlertResult
				if err := alertRows.Scan(&a.ID, &a.Summary, &a.Severity, &a.TenantID, &a.CreatedAt); err == nil {
					resp.Alerts = append(resp.Alerts, a)
				}
			}
		}

		runbookRows, err := pgPool.Query(ctx, `
			SELECT id, name, tenant_id, is_global
			FROM tenant_runbooks
			WHERE (tenant_id = ANY($1) OR is_global = true) AND name ILIKE $2
			ORDER BY name ASC
			LIMIT 8
		`, tenantIDs, likeTerm)
		if err == nil {
			defer runbookRows.Close()
			for runbookRows.Next() {
				var rb SearchRunbookResult
				if err := runbookRows.Scan(&rb.ID, &rb.Name, &rb.TenantID, &rb.IsGlobal); err == nil {
					resp.Runbooks = append(resp.Runbooks, rb)
				}
			}
		}

		// tenantIDs is already validated against the caller's real access, so no additional
		// membership check is needed here — just filter to the same authorized set.
		tenantRows, err := pgPool.Query(ctx, `
			SELECT id, name FROM tenants
			WHERE id = ANY($1) AND status = 'active' AND name ILIKE $2
			ORDER BY name ASC
			LIMIT 8
		`, tenantIDs, likeTerm)
		if err == nil {
			defer tenantRows.Close()
			for tenantRows.Next() {
				var t SearchTenantResult
				if err := tenantRows.Scan(&t.ID, &t.Name); err == nil {
					resp.Tenants = append(resp.Tenants, t)
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
