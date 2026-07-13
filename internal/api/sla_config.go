package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SLATarget is one severity's SLA configuration, in minutes.
type SLATarget struct {
	Severity          string  `json:"severity"`
	MTTATargetMinutes float64 `json:"mtta_target_minutes"`
	MTTRTargetMinutes float64 `json:"mttr_target_minutes"`
}

// defaultSLATargets are the built-in per-severity targets used when a tenant has not customized a
// severity. These MTTR values (15/30/120/480) are the canonical incident-response compliance
// targets — still mirrored in frontend/src/components/alerts/sla-countdown.tsx and
// workers/sla_report_generator.py (Go can't import TS/Python, hence the duplication). MTTA is a
// proportionally tighter acknowledgment target.
var defaultSLATargets = map[string]SLATarget{
	"fatal":    {Severity: "fatal", MTTATargetMinutes: 5, MTTRTargetMinutes: 15},
	"critical": {Severity: "critical", MTTATargetMinutes: 10, MTTRTargetMinutes: 30},
	"warning":  {Severity: "warning", MTTATargetMinutes: 30, MTTRTargetMinutes: 120},
	"info":     {Severity: "info", MTTATargetMinutes: 120, MTTRTargetMinutes: 480},
}

// mergeSLATargets returns the effective per-severity targets: the built-in defaults overridden by
// whatever the tenant customized. Pure and unit-testable.
func mergeSLATargets(overrides map[string]SLATarget) map[string]SLATarget {
	out := make(map[string]SLATarget, len(defaultSLATargets))
	for sev, t := range defaultSLATargets {
		out[sev] = t
	}
	for sev, t := range overrides {
		if _, known := out[sev]; known {
			out[sev] = SLATarget{Severity: sev, MTTATargetMinutes: t.MTTATargetMinutes, MTTRTargetMinutes: t.MTTRTargetMinutes}
		}
	}
	return out
}

// validateSLATarget checks a single override row. Pure.
func validateSLATarget(t SLATarget) error {
	if _, ok := defaultSLATargets[t.Severity]; !ok {
		return fmt.Errorf("invalid severity %q (expected fatal/critical/warning/info)", t.Severity)
	}
	if t.MTTATargetMinutes <= 0 || t.MTTRTargetMinutes <= 0 {
		return fmt.Errorf("targets must be positive for severity %q", t.Severity)
	}
	if t.MTTATargetMinutes > t.MTTRTargetMinutes {
		return fmt.Errorf("mtta_target must not exceed mttr_target for severity %q", t.Severity)
	}
	return nil
}

// loadTenantSLATargets reads the tenant's SLA overrides (inside its RLS context) and merges them
// onto the defaults, returning the effective targets for all four severities.
func loadTenantSLATargets(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (map[string]SLATarget, error) {
	rows, err := tx.Query(ctx, `SELECT severity, mtta_target_minutes, mttr_target_minutes FROM tenant_sla WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	overrides := map[string]SLATarget{}
	for rows.Next() {
		var t SLATarget
		if err := rows.Scan(&t.Severity, &t.MTTATargetMinutes, &t.MTTRTargetMinutes); err != nil {
			return nil, err
		}
		overrides[t.Severity] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return mergeSLATargets(overrides), nil
}

// LoadEffectiveSLATargets is the exported wrapper the worker's SLA-escalation monitor uses to read a
// tenant's effective per-severity SLA targets (defaults overridden by tenant_sla) inside the tenant
// RLS transaction. Kept here so the canonical targets stay defined in one place.
func LoadEffectiveSLATargets(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (map[string]SLATarget, error) {
	return loadTenantSLATargets(ctx, tx, tenantID)
}

// orderedSLATargets returns the effective targets as a fixed-order slice (fatal→info).
func orderedSLATargets(effective map[string]SLATarget) []SLATarget {
	out := make([]SLATarget, 0, len(severityOrder))
	for _, sev := range severityOrder {
		if t, ok := effective[sev]; ok {
			out = append(out, t)
		}
	}
	return out
}

// HandleGetSLAConfig returns the tenant's effective SLA targets (defaults merged with overrides).
func HandleGetSLAConfig(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var effective map[string]SLATarget
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var e error
			effective, e = loadTenantSLATargets(ctx, tx, tenantID)
			return e
		})
		if err != nil {
			http.Error(w, "Failed to load SLA config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"targets": orderedSLATargets(effective)})
	}
}

// HandleSetSLAConfig upserts per-severity SLA overrides for the tenant. Gated at the route level to
// tenant admins. Any severity omitted from the body keeps its current value (defaults if never set).
func HandleSetSLAConfig(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var body struct {
			Targets []SLATarget `json:"targets"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}
		if len(body.Targets) == 0 {
			http.Error(w, "Bad Request: at least one target is required", http.StatusBadRequest)
			return
		}
		for _, t := range body.Targets {
			if verr := validateSLATarget(t); verr != nil {
				http.Error(w, "Bad Request: "+verr.Error(), http.StatusBadRequest)
				return
			}
		}

		var effective map[string]SLATarget
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			for _, t := range body.Targets {
				if _, e := tx.Exec(ctx, `
					INSERT INTO tenant_sla (tenant_id, severity, mtta_target_minutes, mttr_target_minutes, updated_at)
					VALUES ($1, $2, $3, $4, NOW())
					ON CONFLICT (tenant_id, severity)
					DO UPDATE SET mtta_target_minutes = EXCLUDED.mtta_target_minutes,
					              mttr_target_minutes = EXCLUDED.mttr_target_minutes,
					              updated_at = NOW()
				`, tenantID, t.Severity, t.MTTATargetMinutes, t.MTTRTargetMinutes); e != nil {
					return e
				}
			}
			var e error
			effective, e = loadTenantSLATargets(ctx, tx, tenantID)
			return e
		})
		if err != nil {
			http.Error(w, "Failed to save SLA config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"targets": orderedSLATargets(effective)})
	}
}
