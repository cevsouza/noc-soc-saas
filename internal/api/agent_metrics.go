package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Raw SNMP metric samples (slice 3): the agent reports every polled OID's value each cycle so the NOC
// console can graph them over time. Distinct from /agent/events (which carries only threshold
// breaches as alerts).

// MetricSample is one polled value.
type MetricSample struct {
	TargetID string  `json:"target_id"`
	OID      string  `json:"oid"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
}

// AgentMetricsRequest is the agent's metric push batch.
type AgentMetricsRequest struct {
	AgentID uuid.UUID      `json:"agent_id"`
	Samples []MetricSample `json:"samples"`
}

// HandleAgentMetrics ingests a batch of metric samples (API-key auth) and refreshes agent liveness.
func HandleAgentMetrics(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var req AgentMetricsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: invalid payload", http.StatusBadRequest)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		accepted := 0
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			for _, s := range req.Samples {
				if s.OID == "" || s.Label == "" {
					continue
				}
				var targetID *uuid.UUID
				if id, perr := uuid.Parse(s.TargetID); perr == nil {
					targetID = &id
				}
				if _, e := tx.Exec(ctx, `INSERT INTO agent_metrics (tenant_id, target_id, oid, label, value) VALUES ($1,$2,$3,$4,$5)`,
					tenantID, targetID, s.OID, s.Label, s.Value); e != nil {
					return e
				}
				accepted++
			}
			return nil
		})
		if err != nil {
			http.Error(w, "Failed to store metrics", http.StatusInternalServerError)
			return
		}

		touchAgent(r, pgPool, tenantID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"accepted": accepted})
	}
}

// MetricSeriesEntry is one graph point.
type MetricSeriesEntry struct {
	TS    time.Time `json:"ts"`
	Value float64   `json:"value"`
}

// MetricCatalogEntry names an available series for the UI dropdowns.
type MetricCatalogEntry struct {
	TargetID *uuid.UUID `json:"target_id"`
	OID      string     `json:"oid"`
	Label    string     `json:"label"`
}

// HandleGetMetricsCatalog lists the tenant's available (target, oid, label) series.
func HandleGetMetricsCatalog(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		out := make([]MetricCatalogEntry, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `SELECT DISTINCT target_id, oid, label FROM agent_metrics WHERE tenant_id = $1 ORDER BY label`, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var c MetricCatalogEntry
				if e := rows.Scan(&c.TargetID, &c.OID, &c.Label); e != nil {
					return e
				}
				out = append(out, c)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to load metrics catalog", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// HandleGetMetricsSeries returns a metric's samples in the last N hours (?target_id=&oid=&hours=).
func HandleGetMetricsSeries(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		targetID, perr := uuid.Parse(r.URL.Query().Get("target_id"))
		oid := r.URL.Query().Get("oid")
		if perr != nil || oid == "" {
			http.Error(w, "Bad Request: target_id and oid are required", http.StatusBadRequest)
			return
		}
		hours := 6
		if h, e := strconv.Atoi(r.URL.Query().Get("hours")); e == nil && h >= 1 && h <= 168 {
			hours = h
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		out := make([]MetricSeriesEntry, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `
				SELECT ts, value FROM agent_metrics
				WHERE tenant_id = $1 AND target_id = $2 AND oid = $3 AND ts > NOW() - make_interval(hours => $4)
				ORDER BY ts ASC
				LIMIT 3000
			`, tenantID, targetID, oid, hours)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var m MetricSeriesEntry
				if e := rows.Scan(&m.TS, &m.Value); e != nil {
					return e
				}
				out = append(out, m)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to load metric series", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}
