package api

import (
	"encoding/json"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Compliance & governance report (B4). A read-only posture summary an MSSP can show a client: the
// tenant's data-retention policy, data inventory, audit-log integrity, and the platform-wide security
// guarantees (per-tenant RLS, per-tenant encryption, append-only audit). Everything is computed from
// existing data + documented platform facts — no new storage.

// ComplianceReport is the governance posture for one tenant.
type ComplianceReport struct {
	GeneratedAt time.Time `json:"generated_at"`

	// Data retention.
	AlertsRetentionEnabled bool `json:"alerts_retention_enabled"`
	AlertsRetentionDays    int  `json:"alerts_retention_days"`

	// Data inventory.
	TotalAlerts    int        `json:"total_alerts"`
	OldestAlert    *time.Time `json:"oldest_alert"`
	TotalIncidents int        `json:"total_incidents"`

	// Audit trail.
	AuditEntries    int        `json:"audit_entries"`
	OldestAudit     *time.Time `json:"oldest_audit"`
	AuditAppendOnly bool       `json:"audit_append_only"`

	// Platform security guarantees (constant facts, surfaced for the report).
	TenantIsolationRLS  bool `json:"tenant_isolation_rls"`
	PerTenantEncryption bool `json:"per_tenant_encryption"`

	// Governance surface.
	SuppressionRules int  `json:"suppression_rules"`
	SLACustomized    bool `json:"sla_customized"`
}

// HandleGetComplianceReport returns the compliance posture for the caller's tenant. Read-only; runs
// inside the tenant-scoped RLS transaction.
func HandleGetComplianceReport(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		report := ComplianceReport{
			GeneratedAt: time.Now(),
			// Documented platform guarantees: FORCE RLS + non-owner runtime role (Fase 0), per-tenant
			// HKDF-derived vault keys (Fase 5.2), append-only audit trigger + REVOKE (Fase 8/4a).
			AuditAppendOnly:     true,
			TenantIsolationRLS:  true,
			PerTenantEncryption: true,
		}

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// Retention policy (opt-in; no row = disabled).
			var days int
			e := tx.QueryRow(ctx, `SELECT alerts_retention_days FROM tenant_retention WHERE tenant_id = $1`, tenantID).Scan(&days)
			if e == nil {
				report.AlertsRetentionEnabled = true
				report.AlertsRetentionDays = days
			} else if e != pgx.ErrNoRows {
				return e
			}

			if e := tx.QueryRow(ctx, `SELECT COUNT(*), MIN(created_at) FROM alerts WHERE tenant_id = $1`, tenantID).Scan(&report.TotalAlerts, &report.OldestAlert); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx, `SELECT COUNT(*) FROM incidents WHERE tenant_id = $1`, tenantID).Scan(&report.TotalIncidents); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx, `SELECT COUNT(*), MIN(created_at) FROM audit_logs WHERE tenant_id = $1`, tenantID).Scan(&report.AuditEntries, &report.OldestAudit); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx, `SELECT COUNT(*) FROM tenant_suppression_rules WHERE tenant_id = $1`, tenantID).Scan(&report.SuppressionRules); e != nil {
				return e
			}
			if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant_sla WHERE tenant_id = $1)`, tenantID).Scan(&report.SLACustomized); e != nil {
				return e
			}
			return nil
		})
		if err != nil {
			http.Error(w, "Failed to build compliance report", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(report)
	}
}
