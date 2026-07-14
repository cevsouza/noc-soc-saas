package api

import (
	"encoding/json"
	"log"
	"net/http"

	"noc-api/internal/audit"
	"noc-api/internal/middleware"
	"noc-api/internal/threatintel"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Cross-tenant threat intel API (Backlog B6 fatia 1). Tenant-plane surface over the global,
// anonymized aggregate. A tenant only sees shared intel once it has opted in — the same opt-in that
// makes it a contributor. The response never reveals which tenants contributed; it is aggregate only.

// ThreatIntelResponse is what GET /api/v1/threat-intel returns.
type ThreatIntelResponse struct {
	OptedIn    bool                        `json:"opted_in"`
	Indicators []threatintel.SharedIndicator `json:"indicators"`
}

// HandleGetThreatIntel returns the caller tenant's opt-in status and, when opted in, the top shared
// indicators. When the tenant has not opted in, indicators is empty (no free-riding on the network).
func HandleGetThreatIntel(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}

		var optedIn bool
		if err := pgPool.QueryRow(r.Context(), `SELECT threat_intel_opt_in FROM tenants WHERE id = $1`, tenantID).Scan(&optedIn); err != nil {
			http.Error(w, "Failed to read opt-in status", http.StatusInternalServerError)
			return
		}

		resp := ThreatIntelResponse{OptedIn: optedIn, Indicators: []threatintel.SharedIndicator{}}
		if optedIn {
			inds, err := threatintel.TopShared(r.Context(), pgPool, 100)
			if err != nil {
				http.Error(w, "Failed to load threat intel", http.StatusInternalServerError)
				return
			}
			resp.Indicators = inds
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// HandleSetThreatIntelOptIn toggles the caller tenant's participation in the shared threat-intel
// network. Admin-gated at the route. Opting out stops future contributions and hides shared intel;
// past anonymized contributions remain in the aggregate (they carry no tenant identity).
func HandleSetThreatIntelOptIn(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		var body struct {
			OptIn bool `json:"opt_in"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}
		if _, err := pgPool.Exec(r.Context(), `UPDATE tenants SET threat_intel_opt_in = $1 WHERE id = $2`, body.OptIn, tenantID); err != nil {
			http.Error(w, "Failed to update opt-in", http.StatusInternalServerError)
			return
		}

		var actorID uuid.UUID
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			actorID = claims.UserID
		}
		audit.Record(r.Context(), pgPool, audit.Entry{
			TenantID:  tenantID,
			UserID:    actorID,
			Action:    "threat_intel.opt_in.set",
			Resource:  tenantID.String(),
			Details:   map[string]interface{}{"opt_in": body.OptIn},
			IPAddress: r.RemoteAddr,
		})
		log.Printf("[ThreatIntel] tenant %s opt_in=%v", tenantID, body.OptIn)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"opted_in": body.OptIn})
	}
}
