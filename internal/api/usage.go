package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Usage metering (Fase 6): the billing basis + the control-plane vs tenant-plane split.
//
//   - Tenant-plane: GET /api/v1/usage — the caller's own tenant usage (self-service).
//   - Control-plane: GET /api/v1/admin/usage — every tenant's usage aggregated, for the MSSP
//     operator. Platform-admin only (RequireGlobalRole at the route).
//
// alerts is a shared, monthly-partitioned table with FORCE RLS, so cross-tenant aggregation is done
// by iterating tenants and reading each one's counts inside its own RLS context (same approach as
// the watchdog / retention enforcer), not by a single privileged cross-tenant query.

const usageWindowDays = 30

// TenantUsage is one tenant's metered usage over the reporting window.
type TenantUsage struct {
	TenantID           uuid.UUID `json:"tenant_id"`
	TenantName         string    `json:"tenant_name,omitempty"`
	AlertsInWindow     int64     `json:"alerts_in_window"`
	AvgEventsPerDay    float64   `json:"avg_events_per_day"`
	EPS                float64   `json:"eps"`
	TotalAlertsStored  int64     `json:"total_alerts_stored"`
	ActiveUsers        int64     `json:"active_users"`
	ActiveIntegrations int64     `json:"active_integrations"`
	OpenIncidents      int64     `json:"open_incidents"`

	// Billing plan + quota limits (B2 fatia 2). Embedded in the usage roll-up so the control-plane
	// dashboard renders usage-vs-limit in one call. -1 limits mean unlimited; a tenant with no plan
	// row reports the default ("free"). Omitted on the platform Totals aggregate.
	PlanName          string `json:"plan_name,omitempty"`
	MaxAlertsPerMonth int    `json:"max_alerts_per_month,omitempty"`
	MaxIntegrations   int    `json:"max_integrations,omitempty"`
	MaxUsers          int    `json:"max_users,omitempty"`
}

// PlatformUsage is the control-plane roll-up: per-tenant usage plus platform totals.
type PlatformUsage struct {
	WindowDays  int           `json:"window_days"`
	TenantCount int           `json:"tenant_count"`
	Tenants     []TenantUsage `json:"tenants"`
	Totals      TenantUsage   `json:"totals"`
}

// computeEPS derives events-per-second from a count over a window. Pure.
func computeEPS(count int64, windowSeconds float64) float64 {
	if windowSeconds <= 0 {
		return 0
	}
	return float64(count) / windowSeconds
}

// computeTenantUsage gathers one tenant's metered usage. Must run inside the tenant's RLS tx.
func computeTenantUsage(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (TenantUsage, error) {
	u := TenantUsage{TenantID: tenantID}
	windowStart := time.Now().AddDate(0, 0, -usageWindowDays)

	if err := tx.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE tenant_id = $1 AND created_at > $2`, tenantID, windowStart).Scan(&u.AlertsInWindow); err != nil {
		return u, err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM alerts WHERE tenant_id = $1`, tenantID).Scan(&u.TotalAlertsStored); err != nil {
		return u, err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenant_users WHERE tenant_id = $1`, tenantID).Scan(&u.ActiveUsers); err != nil {
		return u, err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tenant_integrations WHERE tenant_id = $1 AND status = 'active'`, tenantID).Scan(&u.ActiveIntegrations); err != nil {
		return u, err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM incidents WHERE tenant_id = $1 AND status <> 'resolved'`, tenantID).Scan(&u.OpenIncidents); err != nil {
		return u, err
	}

	u.AvgEventsPerDay = float64(u.AlertsInWindow) / float64(usageWindowDays)
	u.EPS = computeEPS(u.AlertsInWindow, float64(usageWindowDays)*86400)

	// Attach the tenant's billing plan + limits so the dashboard shows usage-vs-limit (B2 fatia 2).
	plan, err := loadTenantPlan(ctx, tx, tenantID)
	if err != nil {
		return u, err
	}
	u.PlanName = plan.PlanName
	u.MaxAlertsPerMonth = plan.MaxAlertsPerMonth
	u.MaxIntegrations = plan.MaxIntegrations
	u.MaxUsers = plan.MaxUsers
	return u, nil
}

// HandleGetTenantUsage returns the caller's own tenant usage (tenant-plane, any authenticated user).
func HandleGetTenantUsage(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var usage TenantUsage
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var e error
			usage, e = computeTenantUsage(ctx, tx, tenantID)
			return e
		})
		if err != nil {
			log.Printf("[API Error] tenant usage: %v", err)
			http.Error(w, "Failed to compute usage", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(usage)
	}
}

// HandleGetPlatformUsage returns every tenant's usage plus platform totals (control-plane). Gated to
// platform admins at the route. Enumerates tenants from the registry (not RLS-forced) then meters
// each inside its own RLS context.
func HandleGetPlatformUsage(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		type tref struct {
			id   uuid.UUID
			name string
		}
		var tenants []tref
		rows, err := pgPool.Query(ctx, `SELECT id, name FROM tenants WHERE status = 'active' ORDER BY name`)
		if err != nil {
			http.Error(w, "Failed to list tenants", http.StatusInternalServerError)
			return
		}
		for rows.Next() {
			var t tref
			if err := rows.Scan(&t.id, &t.name); err != nil {
				rows.Close()
				http.Error(w, "Failed to scan tenants", http.StatusInternalServerError)
				return
			}
			tenants = append(tenants, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			http.Error(w, "Failed to read tenants", http.StatusInternalServerError)
			return
		}

		out := PlatformUsage{WindowDays: usageWindowDays, Tenants: make([]TenantUsage, 0, len(tenants))}
		for _, t := range tenants {
			tctx := db.WithTenantID(ctx, t.id)
			var u TenantUsage
			e := db.ExecuteInTenantTx(tctx, pgPool, func(tx pgx.Tx) error {
				var err error
				u, err = computeTenantUsage(tctx, tx, t.id)
				return err
			})
			if e != nil {
				log.Printf("[API Error] platform usage for tenant %s: %v", t.id, e)
				continue // skip a failing tenant rather than failing the whole report
			}
			u.TenantName = t.name
			out.Tenants = append(out.Tenants, u)

			out.Totals.AlertsInWindow += u.AlertsInWindow
			out.Totals.TotalAlertsStored += u.TotalAlertsStored
			out.Totals.ActiveUsers += u.ActiveUsers
			out.Totals.ActiveIntegrations += u.ActiveIntegrations
			out.Totals.OpenIncidents += u.OpenIncidents
		}
		out.TenantCount = len(out.Tenants)
		out.Totals.AvgEventsPerDay = float64(out.Totals.AlertsInWindow) / float64(usageWindowDays)
		out.Totals.EPS = computeEPS(out.Totals.AlertsInWindow, float64(usageWindowDays)*86400)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
