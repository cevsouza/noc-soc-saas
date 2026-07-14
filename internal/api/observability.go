package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"noc-api/internal/db"
	"noc-api/internal/loki"
	"noc-api/internal/middleware"
	"noc-api/internal/repository"
	"noc-api/internal/security"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Observability helpers for the alert detail: a live Loki re-fetch and a Grafana embed-URL builder.
// Both are read-only, tenant-scoped, and reuse the same per-tenant config the worker already uses.

// HandleGetHostLogs re-fetches a host's logs from Grafana Loki on demand (the "Recarregar" button in
// the Loki Logs tab). The stored logs are a snapshot from when the alert fired; this pulls the
// current window. Falls back to the same mock logs the worker uses when Loki isn't configured.
func HandleGetHostLogs(pgPool *pgxpool.Pool) http.HandlerFunc {
	client := loki.NewLokiClient(pgPool)
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		host := strings.TrimSpace(r.URL.Query().Get("host"))
		if host == "" {
			http.Error(w, "Bad Request: host is required", http.StatusBadRequest)
			return
		}
		logs, err := client.FetchHostLogs(r.Context(), tenantID, host)
		if err != nil {
			logs = []string{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"logs": logs})
	}
}

// HandleGrafanaEmbed returns the embed URL for the tenant's Grafana dashboard, with {host}/{from}/{to}
// substituted for the incident's host and time window. The template is stored per tenant in the vault
// under "grafana_dashboard_url"; when it isn't configured the URL comes back empty so the UI can show
// the fallback + setup instructions.
func HandleGrafanaEmbed(pgPool *pgxpool.Pool) http.HandlerFunc {
	vaultRepo := repository.NewPostgresVaultRepository()
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		host := strings.TrimSpace(r.URL.Query().Get("host"))
		from := strings.TrimSpace(r.URL.Query().Get("from"))
		to := strings.TrimSpace(r.URL.Query().Get("to"))

		masterKey, mkErr := security.GetMasterKey()
		var template string
		ctx := db.WithTenantID(r.Context(), tenantID)
		_ = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			sec, e := vaultRepo.GetSecretByKey(ctx, tx, "grafana_dashboard_url")
			if e != nil || sec == nil {
				return nil // not configured — leave template empty
			}
			if mkErr == nil {
				if dec, derr := security.DecryptForTenant(sec.EncryptedValue, sec.Nonce, masterKey, tenantID); derr == nil {
					template = strings.TrimSpace(string(dec))
				}
			}
			return nil
		})

		url := ""
		if template != "" {
			url = strings.NewReplacer("{host}", host, "{from}", from, "{to}", to).Replace(template)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"url": url})
	}
}
