package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Analyst-productivity KPI (K3): who did how much over the window, derived from the append-only
// audit_logs (which stamps user_id on every sensitive action since Fase 8/4a). This is the "people"
// dimension the operational report was missing — attributable throughput per analyst. It counts
// deliberate actions (runbook approve/reject/execute, containment approve/reject, secret writes),
// not passive views. System-initiated actions (NULL user_id) are excluded.

const analystWindowDays = 30

// ActionCount is one action type and how many times an analyst performed it in the window.
type ActionCount struct {
	Action string `json:"action"`
	Count  int    `json:"count"`
}

// AnalystProductivity is one analyst's attributable action volume over the window.
type AnalystProductivity struct {
	UserID       uuid.UUID     `json:"user_id"`
	Name         string        `json:"name"`
	Email        string        `json:"email"`
	TotalActions int           `json:"total_actions"`
	ByAction     []ActionCount `json:"by_action"`
}

// AnalystStats is the per-analyst productivity bundle for a tenant.
type AnalystStats struct {
	WindowDays   int                   `json:"window_days"`
	TotalActions int                   `json:"total_actions"`
	Analysts     []AnalystProductivity `json:"analysts"`
}

// analystActionRow is one (user, action, count) row from the aggregation query — the raw input to the
// pure assembler below.
type analystActionRow struct {
	UserID uuid.UUID
	Name   string
	Email  string
	Action string
	Count  int
}

// aggregateAnalystRows folds per-(user,action) rows into per-analyst records: total actions, the
// per-action breakdown (most frequent first), analysts sorted by total desc, and the grand total.
// Pure and unit-tested — no DB dependency.
func aggregateAnalystRows(rows []analystActionRow) ([]AnalystProductivity, int) {
	order := []uuid.UUID{}
	byUser := map[uuid.UUID]*AnalystProductivity{}
	total := 0
	for _, r := range rows {
		a, ok := byUser[r.UserID]
		if !ok {
			a = &AnalystProductivity{UserID: r.UserID, Name: r.Name, Email: r.Email}
			byUser[r.UserID] = a
			order = append(order, r.UserID)
		}
		a.TotalActions += r.Count
		a.ByAction = append(a.ByAction, ActionCount{Action: r.Action, Count: r.Count})
		total += r.Count
	}
	out := make([]AnalystProductivity, 0, len(order))
	for _, id := range order {
		a := byUser[id]
		sort.SliceStable(a.ByAction, func(i, j int) bool { return a.ByAction[i].Count > a.ByAction[j].Count })
		out = append(out, *a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TotalActions > out[j].TotalActions })
	return out, total
}

// HandleGetAnalystStats returns per-analyst productivity for the caller's tenant over the window.
// Read-only; runs inside the tenant-scoped RLS transaction, and the explicit tenant_id filter on
// audit_logs is defense-in-depth.
func HandleGetAnalystStats(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var rows []analystActionRow
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			window := fmt.Sprintf("%d days", analystWindowDays)
			qrows, err := tx.Query(ctx, `
				SELECT a.user_id, u.name, u.email, a.action, COUNT(*) AS c
				FROM audit_logs a
				JOIN users u ON u.id = a.user_id
				WHERE a.tenant_id = $1 AND a.created_at >= NOW() - $2::interval AND a.user_id IS NOT NULL
				GROUP BY a.user_id, u.name, u.email, a.action
			`, tenantID, window)
			if err != nil {
				return err
			}
			defer qrows.Close()
			for qrows.Next() {
				var ar analystActionRow
				if err := qrows.Scan(&ar.UserID, &ar.Name, &ar.Email, &ar.Action, &ar.Count); err != nil {
					return err
				}
				rows = append(rows, ar)
			}
			return qrows.Err()
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to compute analyst stats: %v", err), http.StatusInternalServerError)
			return
		}

		analysts, total := aggregateAnalystRows(rows)
		stats := AnalystStats{WindowDays: analystWindowDays, TotalActions: total, Analysts: analysts}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}
