package api

import (
	"encoding/json"
	"log"
	"net/http"

	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"noc-api/internal/ocsf"
	"noc-api/internal/repository"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HandleGetAlertsOCSF exports the tenant's recent alerts as OCSF Detection Finding events
// (class_uid 2004), for ingestion by OCSF-native consumers (security data lakes, SIEMs). Same
// tenant-scoping as HandleListAlerts; read-only. Optional ?limit= (default 100, max 500).
func HandleGetAlertsOCSF(pgPool *pgxpool.Pool, alertRepo repository.AlertRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		limit := parseLimit(r.URL.Query().Get("limit"), 100, 500)

		var alerts []*model.Alert
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var e error
			alerts, e = alertRepo.List(ctx, tx, tenantID, limit, 0)
			return e
		})
		if err != nil {
			log.Printf("[API Error] Failed to list alerts for OCSF export: %v", err)
			http.Error(w, "Internal Server Error: failed to load alerts", http.StatusInternalServerError)
			return
		}

		findings := make([]ocsf.DetectionFinding, 0, len(alerts))
		for _, a := range alerts {
			findings = append(findings, ocsf.FromAlert(a))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(findings)
	}
}

// parseLimit parses a positive integer limit within [1, max], falling back to def.
func parseLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
		if n > max {
			return max
		}
	}
	if n < 1 {
		return def
	}
	return n
}
