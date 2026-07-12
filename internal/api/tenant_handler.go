package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/middleware"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TenantResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

type CreateTenantRequest struct {
	Name string `json:"name"`
}

// HandleGetTenants returns all active tenants in the platform.
func HandleGetTenants(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		rows, err := pgPool.Query(ctx, "SELECT id, name, slug FROM tenants WHERE status = 'active' ORDER BY name")
		if err != nil {
			http.Error(w, "Failed to query tenants", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var list []TenantResponse
		for rows.Next() {
			var t TenantResponse
			if err := rows.Scan(&t.ID, &t.Name, &t.Slug); err != nil {
				http.Error(w, "Failed to scan tenants", http.StatusInternalServerError)
				return
			}
			list = append(list, t)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	}
}

// HandleCreateTenant allows admins to register a new tenant and auto-associates the creator as tenant admin.
func HandleCreateTenant(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateTenantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			http.Error(w, "Tenant name is required", http.StatusBadRequest)
			return
		}

		// Generate simple slug (lowercase, replace spaces with hyphens)
		slug := strings.ToLower(req.Name)
		slug = strings.ReplaceAll(slug, " ", "-")
		// Clean special characters
		reg := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ç", "c")
		slug = reg.Replace(slug)

		ctx := r.Context()
		tenantID := uuid.New()

		// Begin transaction to create tenant and associate the creator
		tx, err := pgPool.Begin(ctx)
		if err != nil {
			http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		// 1. Insert tenant
		queryTenant := "INSERT INTO tenants (id, name, slug, status, created_at, updated_at) VALUES ($1, $2, $3, 'active', $4, $5)"
		now := time.Now()
		_, err = tx.Exec(ctx, queryTenant, tenantID, req.Name, slug, now, now)
		if err != nil {
			http.Error(w, "Failed to insert tenant (name/slug might already exist)", http.StatusConflict)
			return
		}

		// 2. Associate the admin user creating this tenant
		claims, ok := middleware.ClaimsFromContext(ctx)
		if ok {
			queryAssociate := "INSERT INTO tenant_users (tenant_id, user_id, role, created_at) VALUES ($1, $2, 'admin', $3)"
			_, err = tx.Exec(ctx, queryAssociate, tenantID, claims.UserID, now)
			if err != nil {
				// If association fails, rollback and fail
				http.Error(w, "Failed to associate user with new tenant", http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(TenantResponse{
			ID:   tenantID,
			Name: req.Name,
			Slug: slug,
		})
	}
}

// HandleGetPublicTenants returns all active tenants names and IDs for public selectors
func HandleGetPublicTenants(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		rows, err := pgPool.Query(ctx, "SELECT id, name FROM tenants WHERE status = 'active' ORDER BY name")
		if err != nil {
			http.Error(w, "Failed to query tenants", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type PublicTenant struct {
			ID   uuid.UUID `json:"id"`
			Name string    `json:"name"`
		}

		list := make([]PublicTenant, 0)
		for rows.Next() {
			var t PublicTenant
			if err := rows.Scan(&t.ID, &t.Name); err != nil {
				http.Error(w, "Failed to scan tenants", http.StatusInternalServerError)
				return
			}
			list = append(list, t)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

type UpdateTenantStyleRequest struct {
	TenantID     uuid.UUID `json:"tenant_id"`
	LogoURL      string    `json:"logo_url"`
	PrimaryColor string    `json:"primary_color"`
}

// HandleUpdateTenantStyle updates logo URL and primary brand color for a tenant.
// Requires the caller to be a platform-wide admin, or an admin of the tenant being modified.
func HandleUpdateTenantStyle(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UpdateTenantStyleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: User claims missing", http.StatusUnauthorized)
			return
		}
		if claims.GlobalRole != model.RoleAdmin && (claims.TenantID != req.TenantID || claims.Role != model.RoleAdmin) {
			http.Error(w, "Forbidden: cannot modify another tenant's style", http.StatusForbidden)
			return
		}

		ctx := r.Context()
		query := `
			UPDATE tenants 
			SET logo_url = $1, primary_color = $2, updated_at = NOW() 
			WHERE id = $3
		`
		_, err := pgPool.Exec(ctx, query, req.LogoURL, req.PrimaryColor, req.TenantID)
		if err != nil {
			http.Error(w, "Failed to update tenant style: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","message":"Tenant visual style updated successfully"}`))
	}
}

// HandleDeleteTenant allows admins to delete a tenant (and cascades its relations, e.g. alerts, rules, etc.)
func HandleDeleteTenant(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantIDStr := r.URL.Query().Get("id")
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "Invalid tenant ID format", http.StatusBadRequest)
			return
		}

		ctx := r.Context()

		// Begin transaction
		tx, err := pgPool.Begin(ctx)
		if err != nil {
			http.Error(w, "Failed to start transaction", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback(ctx)

		// Delete the tenant
		_, err = tx.Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenantID)
		if err != nil {
			http.Error(w, "Failed to delete tenant: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","message":"Tenant deletado com sucesso"}`))
	}
}
