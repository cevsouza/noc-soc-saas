package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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
		var tenantID uuid.UUID
		var ok bool

		// If user is admin, allow overriding tenant_id via query parameter
		tenantIDStr := r.URL.Query().Get("tenant_id")
		if tenantIDStr != "" {
			claims, claimsOk := middleware.ClaimsFromContext(ctx)
			if claimsOk && claims.Role == model.RoleAdmin {
				parsedID, err := uuid.Parse(tenantIDStr)
				if err == nil {
					tenantID = parsedID
					ok = true
				}
			}
		}

		if !ok {
			tenantID, ok = db.TenantIDFromContext(ctx)
		}

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
		var tenantID uuid.UUID
		var ok bool

		// If user is admin, allow overriding tenant_id via query parameter
		tenantIDStr := r.URL.Query().Get("tenant_id")
		if tenantIDStr != "" {
			claims, claimsOk := middleware.ClaimsFromContext(ctx)
			if claimsOk && claims.Role == model.RoleAdmin {
				parsedID, err := uuid.Parse(tenantIDStr)
				if err == nil {
					tenantID = parsedID
					ok = true
				}
			}
		}

		if !ok {
			tenantID, ok = db.TenantIDFromContext(ctx)
		}

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
		var tenantID uuid.UUID
		var ok bool

		// If user is admin, allow overriding tenant_id via query parameter
		tenantIDStr := r.URL.Query().Get("tenant_id")
		if tenantIDStr != "" {
			claims, claimsOk := middleware.ClaimsFromContext(ctx)
			if claimsOk && claims.Role == model.RoleAdmin {
				parsedID, err := uuid.Parse(tenantIDStr)
				if err == nil {
					tenantID = parsedID
					ok = true
				}
			}
		}

		if !ok {
			tenantID, ok = db.TenantIDFromContext(ctx)
		}

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

// HandleGetIntegrationStatus checks connectivity status (heartbeat and errors) in Redis
func HandleGetIntegrationStatus(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		tenantIDStr := r.URL.Query().Get("tenant_id")
		integrationType := r.URL.Query().Get("type")
		
		if tenantIDStr == "" || integrationType == "" {
			http.Error(w, "Bad Request: tenant_id and type are required parameters", http.StatusBadRequest)
			return
		}
		
		tenantID, err := uuid.Parse(tenantIDStr)
		if err != nil {
			http.Error(w, "Bad Request: Invalid tenant ID", http.StatusBadRequest)
			return
		}
		
		// 1. Get heartbeat
		heartbeatKey := "heartbeat:connector:" + tenantID.String() + ":" + integrationType
		lastSeenStr, err := redisClient.Get(ctx, heartbeatKey).Result()
		
		var lastSeen int64
		hasHeartbeat := false
		if err == nil && lastSeenStr != "" {
			_, _ = fmt.Sscanf(lastSeenStr, "%d", &lastSeen)
			hasHeartbeat = true
		}
		
		// 2. Get latest error log
		errorKey := "webhook:error:" + tenantID.String() + ":" + integrationType
		lastError, _ := redisClient.Get(ctx, errorKey).Result()
		
		status := "inactive"
		if hasHeartbeat {
			timeElapsed := time.Now().Unix() - lastSeen
			if timeElapsed < 120 {
				status = "active"
			} else {
				status = "offline"
			}
		}
		
		response := map[string]interface{}{
			"status":      status,
			"last_seen":   lastSeen,
			"last_error":  lastError,
			"has_error":   lastError != "",
		}
		
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}
