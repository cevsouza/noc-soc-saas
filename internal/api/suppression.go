package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// SuppressionRule mirrors a tenant_suppression_rules row (Fase 3/3d).
type SuppressionRule struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	MatchField string     `json:"match_field"`
	MatchValue string     `json:"match_value"`
	StartsAt   *time.Time `json:"starts_at,omitempty"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	Active     bool       `json:"active"`
	CreatedAt  time.Time  `json:"created_at"`
}

// validSuppressionFields are the event fields a rule may match on.
var validSuppressionFields = map[string]bool{
	"event_type": true, "host": true, "summary": true, "source": true, "severity": true,
}

// CreateSuppressionRuleRequest is the POST body.
type CreateSuppressionRuleRequest struct {
	Name       string     `json:"name"`
	MatchField string     `json:"match_field"`
	MatchValue string     `json:"match_value"`
	StartsAt   *time.Time `json:"starts_at,omitempty"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
}

// validateCreateSuppressionRule checks a create request without touching the DB (unit-testable).
func validateCreateSuppressionRule(req CreateSuppressionRuleRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !validSuppressionFields[req.MatchField] {
		return fmt.Errorf("invalid match_field %q (expected event_type/host/summary/source/severity)", req.MatchField)
	}
	if strings.TrimSpace(req.MatchValue) == "" {
		return fmt.Errorf("match_value is required")
	}
	if req.StartsAt != nil && req.EndsAt != nil && req.EndsAt.Before(*req.StartsAt) {
		return fmt.Errorf("ends_at must not be before starts_at")
	}
	return nil
}

func invalidateSuppressionCache(redisClient *redis.Client, tenantID uuid.UUID) {
	if redisClient != nil {
		_ = redisClient.Del(context.Background(), "suppression:rules:"+tenantID.String()).Err() // best-effort
	}
}

// HandleGetSuppressionRules lists the tenant's suppression rules.
func HandleGetSuppressionRules(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]SuppressionRule, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `
				SELECT id, name, match_field, match_value, starts_at, ends_at, active, created_at
				FROM tenant_suppression_rules
				WHERE tenant_id = $1
				ORDER BY created_at DESC
			`, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var s SuppressionRule
				if e := rows.Scan(&s.ID, &s.Name, &s.MatchField, &s.MatchValue, &s.StartsAt, &s.EndsAt, &s.Active, &s.CreatedAt); e != nil {
					return e
				}
				list = append(list, s)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to query suppression rules", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleMutateSuppressionRules creates (POST) or deletes (DELETE ?id=) a rule; both invalidate the
// worker's per-tenant rule cache so the change takes effect immediately. Gated to tenant admins at
// the route level.
func HandleMutateSuppressionRules(pgPool *pgxpool.Pool, redisClient *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)
		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}

		switch r.Method {
		case http.MethodPost:
			var req CreateSuppressionRuleRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "Invalid request payload", http.StatusBadRequest)
				return
			}
			if verr := validateCreateSuppressionRule(req); verr != nil {
				http.Error(w, "Bad Request: "+verr.Error(), http.StatusBadRequest)
				return
			}
			var newID uuid.UUID
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `
					INSERT INTO tenant_suppression_rules (tenant_id, name, match_field, match_value, starts_at, ends_at)
					VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
				`, tenantID, req.Name, req.MatchField, req.MatchValue, req.StartsAt, req.EndsAt).Scan(&newID)
			})
			if err != nil {
				http.Error(w, "Failed to create suppression rule", http.StatusInternalServerError)
				return
			}
			invalidateSuppressionCache(redisClient, tenantID)
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "suppression.create", Resource: req.Name, IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": newID.String()})

		case http.MethodDelete:
			id, perr := uuid.Parse(r.URL.Query().Get("id"))
			if perr != nil {
				http.Error(w, "Bad Request: valid id is required", http.StatusBadRequest)
				return
			}
			var affected int64
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				res, e := tx.Exec(ctx, `DELETE FROM tenant_suppression_rules WHERE id = $1 AND tenant_id = $2`, id, tenantID)
				if e != nil {
					return e
				}
				affected = res.RowsAffected()
				return nil
			})
			if err != nil {
				http.Error(w, "Failed to delete suppression rule", http.StatusInternalServerError)
				return
			}
			if affected == 0 {
				http.Error(w, "Suppression rule not found", http.StatusNotFound)
				return
			}
			invalidateSuppressionCache(redisClient, tenantID)
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "suppression.delete", Resource: id.String(), IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}
}
