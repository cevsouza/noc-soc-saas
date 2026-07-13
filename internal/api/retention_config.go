package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/retention"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RetentionConfig is the tenant's alert-retention policy. Enabled=false means no policy row exists,
// i.e. alerts are kept forever (the safe default).
type RetentionConfig struct {
	Enabled             bool `json:"enabled"`
	AlertsRetentionDays int  `json:"alerts_retention_days"`
}

// loadRetentionConfig reads the tenant's retention policy inside its RLS tx. No row -> disabled.
func loadRetentionConfig(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (RetentionConfig, error) {
	var days int
	err := tx.QueryRow(ctx, `SELECT alerts_retention_days FROM tenant_retention WHERE tenant_id = $1`, tenantID).Scan(&days)
	if errors.Is(err, pgx.ErrNoRows) {
		return RetentionConfig{Enabled: false}, nil
	}
	if err != nil {
		return RetentionConfig{}, err
	}
	return RetentionConfig{Enabled: true, AlertsRetentionDays: days}, nil
}

// HandleGetRetentionConfig returns the tenant's alert-retention policy (or "disabled" if none).
func HandleGetRetentionConfig(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var cfg RetentionConfig
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var e error
			cfg, e = loadRetentionConfig(ctx, tx, tenantID)
			return e
		})
		if err != nil {
			http.Error(w, "Failed to load retention config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cfg)
	}
}

// HandleSetRetentionConfig upserts the tenant's alert-retention window. Route-gated to tenant admins.
func HandleSetRetentionConfig(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		var body struct {
			AlertsRetentionDays int `json:"alerts_retention_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}
		if verr := retention.ValidateDays(body.AlertsRetentionDays); verr != nil {
			http.Error(w, "Bad Request: "+verr.Error(), http.StatusBadRequest)
			return
		}

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `
				INSERT INTO tenant_retention (tenant_id, alerts_retention_days, updated_at)
				VALUES ($1, $2, NOW())
				ON CONFLICT (tenant_id) DO UPDATE SET alerts_retention_days = EXCLUDED.alerts_retention_days, updated_at = NOW()
			`, tenantID, body.AlertsRetentionDays)
			return e
		})
		if err != nil {
			http.Error(w, "Failed to save retention config", http.StatusInternalServerError)
			return
		}

		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}
		audit.Record(ctx, pgPool, audit.Entry{
			TenantID: tenantID, UserID: actorID,
			Action:    "retention.set",
			Resource:  tenantID.String(),
			Details:   map[string]interface{}{"alerts_retention_days": body.AlertsRetentionDays},
			IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(RetentionConfig{Enabled: true, AlertsRetentionDays: body.AlertsRetentionDays})
	}
}

// HandleEnforceRetention runs retention enforcement immediately for the tenant (manual ops lever;
// also useful for on-demand cleanup). Route-gated to tenant admins. No policy -> nothing deleted.
func HandleEnforceRetention(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		var deleted int64
		var cfg RetentionConfig
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var e error
			cfg, e = loadRetentionConfig(ctx, tx, tenantID)
			if e != nil || !cfg.Enabled {
				return e
			}
			deleted, e = retention.EnforceForTenant(ctx, tx, tenantID, cfg.AlertsRetentionDays)
			return e
		})
		if err != nil {
			http.Error(w, "Failed to enforce retention", http.StatusInternalServerError)
			return
		}

		if cfg.Enabled && deleted > 0 {
			var actorID uuid.UUID
			if claims != nil {
				actorID = claims.UserID
			}
			audit.Record(ctx, pgPool, audit.Entry{
				TenantID: tenantID, UserID: actorID,
				Action:    "retention.enforce",
				Resource:  tenantID.String(),
				Details:   map[string]interface{}{"deleted": deleted, "alerts_retention_days": cfg.AlertsRetentionDays},
				IPAddress: r.RemoteAddr,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"enabled": cfg.Enabled, "deleted": deleted})
	}
}
