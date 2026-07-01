package api

import (
	"encoding/json"
	"net/http"
	"noc-api/internal/db"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IntegrationResponse struct {
	ID        uuid.UUID              `json:"id"`
	TenantID  uuid.UUID              `json:"tenant_id"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status"`
	Settings  map[string]interface{} `json:"settings"`
	CreatedAt time.Time              `json:"created_at"`
}

type CreateIntegrationRequest struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Status   string                 `json:"status,omitempty"`
	Settings map[string]interface{} `json:"settings,omitempty"`
}

// HandleGetIntegrations returns the integrations active for the authenticated tenant context
func HandleGetIntegrations(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenantID, ok := db.TenantIDFromContext(ctx)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		rows, err := pgPool.Query(ctx, "SELECT id, tenant_id, name, type, status, settings, created_at FROM tenant_integrations WHERE tenant_id = $1 ORDER BY created_at DESC", tenantID)
		if err != nil {
			http.Error(w, "Failed to query integrations", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		list := make([]IntegrationResponse, 0)
		for rows.Next() {
			var item IntegrationResponse
			var settingsJSON []byte
			if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Type, &item.Status, &settingsJSON, &item.CreatedAt); err != nil {
				http.Error(w, "Failed to scan integration", http.StatusInternalServerError)
				return
			}
			_ = json.Unmarshal(settingsJSON, &item.Settings)
			if item.Settings == nil {
				item.Settings = make(map[string]interface{})
			}
			list = append(list, item)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleCreateIntegration allows admins to associate a new integration configuration with the tenant
func HandleCreateIntegration(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenantID, ok := db.TenantIDFromContext(ctx)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		var req CreateIntegrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		if req.Name == "" || req.Type == "" {
			http.Error(w, "Name and Type are required fields", http.StatusBadRequest)
			return
		}

		if req.Status == "" {
			req.Status = "active"
		}
		if req.Settings == nil {
			req.Settings = make(map[string]interface{})
		}
		settingsBytes, err := json.Marshal(req.Settings)
		if err != nil {
			http.Error(w, "Invalid settings format", http.StatusBadRequest)
			return
		}

		integrationID := uuid.New()
		now := time.Now()

		_, err = pgPool.Exec(ctx,
			"INSERT INTO tenant_integrations (id, tenant_id, name, type, status, settings, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)",
			integrationID, tenantID, req.Name, req.Type, req.Status, settingsBytes, now,
		)
		if err != nil {
			http.Error(w, "Failed to create integration", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(IntegrationResponse{
			ID:        integrationID,
			TenantID:  tenantID,
			Name:      req.Name,
			Type:      req.Type,
			Status:    req.Status,
			Settings:  req.Settings,
			CreatedAt: now,
		})
	}
}

// HandleDeleteIntegration allows admins to remove an integration configuration
func HandleDeleteIntegration(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenantID, ok := db.TenantIDFromContext(ctx)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}

		idStr := r.URL.Query().Get("id")
		if idStr == "" {
			http.Error(w, "Missing integration ID parameter", http.StatusBadRequest)
			return
		}

		integrationID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "Invalid ID format", http.StatusBadRequest)
			return
		}

		res, err := pgPool.Exec(ctx, "DELETE FROM tenant_integrations WHERE id = $1 AND tenant_id = $2", integrationID, tenantID)
		if err != nil {
			http.Error(w, "Failed to delete integration", http.StatusInternalServerError)
			return
		}

		rowsAffected := res.RowsAffected()
		if rowsAffected == 0 {
			http.Error(w, "Integration not found or unauthorized", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
