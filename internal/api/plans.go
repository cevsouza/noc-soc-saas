package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Billing plans + quota limits (Backlog B2 fatia 2). A control-plane concept: the MSSP operator
// assigns each tenant a named plan; the usage dashboard renders metered usage against these limits.
// The quota foundation the future Stripe billing (B2 fatia 3) prices against.
//
// Limits use the sentinel -1 for "unlimited". A tenant with no row defaults to the "free" preset.

// unlimited is the sentinel limit value meaning "no cap".
const unlimited = -1

// TenantPlan is one tenant's plan assignment and its quota limits.
type TenantPlan struct {
	PlanName          string `json:"plan_name"`
	MaxAlertsPerMonth int    `json:"max_alerts_per_month"`
	MaxIntegrations   int    `json:"max_integrations"`
	MaxUsers          int    `json:"max_users"`
}

// planPresets are the built-in named plans and their limits. Mirrored (names only) in
// frontend/src/types/plan.ts — Go can't import TS, hence the duplication. "enterprise" is unlimited.
var planPresets = map[string]TenantPlan{
	"free":       {PlanName: "free", MaxAlertsPerMonth: 5000, MaxIntegrations: 3, MaxUsers: 3},
	"starter":    {PlanName: "starter", MaxAlertsPerMonth: 50000, MaxIntegrations: 10, MaxUsers: 10},
	"pro":        {PlanName: "pro", MaxAlertsPerMonth: 500000, MaxIntegrations: 50, MaxUsers: 50},
	"enterprise": {PlanName: "enterprise", MaxAlertsPerMonth: unlimited, MaxIntegrations: unlimited, MaxUsers: unlimited},
}

// defaultPlanName is assigned to any tenant that has never been given a plan.
const defaultPlanName = "free"

// defaultTenantPlan is the plan returned for a tenant with no tenant_plans row.
func defaultTenantPlan() TenantPlan {
	return planPresets[defaultPlanName]
}

// resolvePlan builds the plan to persist from a request. plan_name must be a known preset. When the
// request omits explicit limits (all three zero), the preset's limits are used; otherwise the
// provided limits form a custom override on top of the named plan. Pure and unit-testable.
func resolvePlan(planName string, maxAlerts, maxIntegrations, maxUsers int) (TenantPlan, error) {
	preset, ok := planPresets[planName]
	if !ok {
		return TenantPlan{}, fmt.Errorf("invalid plan_name %q (expected free/starter/pro/enterprise)", planName)
	}
	// All-zero means "use the preset limits". Any nonzero (including the -1 unlimited sentinel) is
	// treated as an explicit custom override.
	if maxAlerts == 0 && maxIntegrations == 0 && maxUsers == 0 {
		return preset, nil
	}
	p := TenantPlan{PlanName: planName, MaxAlertsPerMonth: maxAlerts, MaxIntegrations: maxIntegrations, MaxUsers: maxUsers}
	if err := validatePlanLimits(p); err != nil {
		return TenantPlan{}, err
	}
	return p, nil
}

// validatePlanLimits rejects nonsensical custom limits (0 or below -1). Pure.
func validatePlanLimits(p TenantPlan) error {
	for _, v := range []int{p.MaxAlertsPerMonth, p.MaxIntegrations, p.MaxUsers} {
		if v < unlimited || v == 0 {
			return fmt.Errorf("limits must be a positive number or -1 (unlimited)")
		}
	}
	return nil
}

// utilizationPct returns used/limit as a percentage, clamped at 0 for unlimited/invalid limits. Pure.
func utilizationPct(used int64, limit int) float64 {
	if limit <= 0 { // unlimited (-1) or unset
		return 0
	}
	return float64(used) / float64(limit) * 100
}

// isOverLimit reports whether metered usage exceeds a finite limit. Pure.
func isOverLimit(used int64, limit int) bool {
	return limit > 0 && used > int64(limit)
}

// loadTenantPlan reads the tenant's plan (inside its RLS context), falling back to the default plan
// when the tenant has no row yet.
func loadTenantPlan(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (TenantPlan, error) {
	var p TenantPlan
	err := tx.QueryRow(ctx, `
		SELECT plan_name, max_alerts_per_month, max_integrations, max_users
		FROM tenant_plans WHERE tenant_id = $1
	`, tenantID).Scan(&p.PlanName, &p.MaxAlertsPerMonth, &p.MaxIntegrations, &p.MaxUsers)
	if err == pgx.ErrNoRows {
		return defaultTenantPlan(), nil
	}
	if err != nil {
		return TenantPlan{}, err
	}
	return p, nil
}

// HandleSetTenantPlan upserts a tenant's plan. Control-plane: platform-admin only (gated at the
// route). The target tenant comes from the body, and the write runs inside that tenant's RLS
// context so a platform admin can set plans for tenants they aren't a member of.
func HandleSetTenantPlan(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TenantID          string `json:"tenant_id"`
			PlanName          string `json:"plan_name"`
			MaxAlertsPerMonth int    `json:"max_alerts_per_month"`
			MaxIntegrations   int    `json:"max_integrations"`
			MaxUsers          int    `json:"max_users"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}
		tenantID, err := uuid.Parse(body.TenantID)
		if err != nil {
			http.Error(w, "Bad Request: tenant_id must be a valid UUID", http.StatusBadRequest)
			return
		}
		plan, err := resolvePlan(body.PlanName, body.MaxAlertsPerMonth, body.MaxIntegrations, body.MaxUsers)
		if err != nil {
			http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
			return
		}

		ctx := db.WithTenantID(r.Context(), tenantID)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `
				INSERT INTO tenant_plans (tenant_id, plan_name, max_alerts_per_month, max_integrations, max_users, updated_at)
				VALUES ($1, $2, $3, $4, $5, NOW())
				ON CONFLICT (tenant_id)
				DO UPDATE SET plan_name = EXCLUDED.plan_name,
				              max_alerts_per_month = EXCLUDED.max_alerts_per_month,
				              max_integrations = EXCLUDED.max_integrations,
				              max_users = EXCLUDED.max_users,
				              updated_at = NOW()
			`, tenantID, plan.PlanName, plan.MaxAlertsPerMonth, plan.MaxIntegrations, plan.MaxUsers)
			return e
		})
		if err != nil {
			http.Error(w, "Failed to save plan", http.StatusInternalServerError)
			return
		}

		var actorID uuid.UUID
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			actorID = claims.UserID
		}
		audit.Record(r.Context(), pgPool, audit.Entry{
			TenantID:  tenantID,
			UserID:    actorID,
			Action:    "tenant.plan.set",
			Resource:  body.TenantID,
			Details:   map[string]interface{}{"plan_name": plan.PlanName, "max_alerts_per_month": plan.MaxAlertsPerMonth},
			IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(plan)
	}
}
