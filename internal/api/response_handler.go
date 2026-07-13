package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/repository"
	"noc-api/internal/responder"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// responseVaultKeys lists, per integration type, the per-tenant vault secret keys that an
// outbound response action needs. On approval the handler fetches and decrypts whichever of
// these exist and hands them to the responder as a plain map; the responder itself decides
// which are required and applies defaults for the optional ones (see internal/responder).
var responseVaultKeys = map[string][]string{
	"paloalto":    {"paloalto_api_key", "paloalto_base_url", "paloalto_block_tag"},
	"fortinet":    {"fortinet_api_token", "fortinet_base_url", "fortinet_block_group"},
	"crowdstrike": {"crowdstrike_client_id", "crowdstrike_client_secret", "crowdstrike_base_url"},
}

// CreateResponseActionRequest is the body of POST /api/v1/response/request.
type CreateResponseActionRequest struct {
	IntegrationType string  `json:"integration_type"`
	ActionType      string  `json:"action_type"`
	Target          string  `json:"target"`
	IncidentID      *string `json:"incident_id,omitempty"`
	Reason          string  `json:"reason,omitempty"`
}

// ResponseActionResponse is one row returned by GET /api/v1/response/requests.
type ResponseActionResponse struct {
	ID              uuid.UUID  `json:"id"`
	IntegrationType string     `json:"integration_type"`
	ActionType      string     `json:"action_type"`
	Target          string     `json:"target"`
	IncidentID      *uuid.UUID `json:"incident_id,omitempty"`
	Status          string     `json:"status"`
	Reason          string     `json:"reason"`
	RequestedBy     string     `json:"requested_by"`
	Output          string     `json:"output"`
	CreatedAt       time.Time  `json:"created_at"`
}

// validateResponseActionRequest checks a create request against the responder registry without
// touching the database, so it is unit-testable. It returns a cleaned action type and target on
// success, or a user-facing error message on failure.
func validateResponseActionRequest(req CreateResponseActionRequest) (responder.ActionType, string, error) {
	itype := strings.TrimSpace(req.IntegrationType)
	target := strings.TrimSpace(req.Target)
	action := responder.ActionType(strings.TrimSpace(req.ActionType))

	if itype == "" || target == "" || action == "" {
		return "", "", fmt.Errorf("integration_type, action_type and target are required")
	}
	if !responder.Supports(itype, action) {
		return "", "", fmt.Errorf("integration %q does not support action %q", itype, action)
	}
	return action, target, nil
}

// HandleCreateResponseAction files a pending outbound containment request (block/unblock IP,
// contain/lift host). It never executes anything — execution happens only on approval — so this
// is deliberately low-privilege (operator/admin) and just records intent after validating that
// the vendor/action is supported and the integration is active for the tenant.
func HandleCreateResponseAction(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: User claims missing", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req CreateResponseActionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		action, target, verr := validateResponseActionRequest(req)
		if verr != nil {
			http.Error(w, verr.Error(), http.StatusBadRequest)
			return
		}

		var incidentID *uuid.UUID
		if req.IncidentID != nil && strings.TrimSpace(*req.IncidentID) != "" {
			parsed, perr := uuid.Parse(strings.TrimSpace(*req.IncidentID))
			if perr != nil {
				http.Error(w, "Invalid incident_id", http.StatusBadRequest)
				return
			}
			incidentID = &parsed
		}

		requestedBy := claims.Email
		if requestedBy == "" {
			requestedBy = claims.UserID.String()
		}

		var newID uuid.UUID
		var createdAt time.Time
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var active bool
			if err := tx.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM tenant_integrations WHERE tenant_id = $1 AND type = $2 AND status = 'active')`,
				tenantID, req.IntegrationType).Scan(&active); err != nil {
				return err
			}
			if !active {
				return errIntegrationInactive
			}
			return tx.QueryRow(ctx, `
				INSERT INTO response_action_requests
					(tenant_id, incident_id, integration_type, action_type, target, reason, requested_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				RETURNING id, created_at
			`, tenantID, incidentID, req.IntegrationType, string(action), target, req.Reason, requestedBy).Scan(&newID, &createdAt)
		})

		if err == errIntegrationInactive {
			http.Error(w, fmt.Sprintf("integration %q is not active for this tenant", req.IntegrationType), http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "Failed to create response action request", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(ResponseActionResponse{
			ID:              newID,
			IntegrationType: req.IntegrationType,
			ActionType:      string(action),
			Target:          target,
			IncidentID:      incidentID,
			Status:          "pending",
			Reason:          req.Reason,
			RequestedBy:     requestedBy,
			CreatedAt:       createdAt,
		})
	}
}

var errIntegrationInactive = fmt.Errorf("integration inactive")

// HandleGetResponseActions lists outbound containment requests for the tenant (defaults to
// status=pending; pass ?status=all for the full history).
func HandleGetResponseActions(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		statusFilter := r.URL.Query().Get("status")
		if statusFilter == "" {
			statusFilter = "pending"
		}

		list := make([]ResponseActionResponse, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT id, integration_type, action_type, target, incident_id, status, reason, requested_by, output, created_at
				FROM response_action_requests
				WHERE tenant_id = $1 AND ($2 = 'all' OR status = $2)
				ORDER BY created_at DESC
				LIMIT 100
			`, tenantID, statusFilter)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var item ResponseActionResponse
				if err := rows.Scan(&item.ID, &item.IntegrationType, &item.ActionType, &item.Target,
					&item.IncidentID, &item.Status, &item.Reason, &item.RequestedBy, &item.Output, &item.CreatedAt); err != nil {
					return err
				}
				list = append(list, item)
			}
			return rows.Err()
		})

		if err != nil {
			http.Error(w, "Failed to query response actions", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// ResponseActionDecisionRequest is the body of the approve/reject endpoints.
type ResponseActionDecisionRequest struct {
	RequestID uuid.UUID `json:"request_id"`
}

// HandleApproveResponseAction approves a pending containment request and fires the vendor call
// immediately, using the tenant's own vault credentials. A failing vendor call is NOT an HTTP
// error — the request is recorded as status='failed' with the vendor's message in output (same
// convention as the SSH runbook-approval path), so the operator sees exactly what happened.
func HandleApproveResponseAction(pgPool *pgxpool.Pool) http.HandlerFunc {
	vaultRepo := repository.NewPostgresVaultRepository()

	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: User claims missing", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req ResponseActionDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		var finalStatus, output string
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var integrationType, actionType, target, status string
			var incidentID *uuid.UUID
			if err := tx.QueryRow(ctx, `
				SELECT integration_type, action_type, target, incident_id, status
				FROM response_action_requests
				WHERE id = $1 AND tenant_id = $2
				FOR UPDATE
			`, req.RequestID, tenantID).Scan(&integrationType, &actionType, &target, &incidentID, &status); err != nil {
				return err
			}
			if status != "pending" {
				return fmt.Errorf("response action is no longer pending (status: %s)", status)
			}

			resp, ok := responder.Get(integrationType)
			if !ok {
				finalStatus = "failed"
				output = fmt.Sprintf("no responder registered for integration %q", integrationType)
			} else {
				// Resolve the tenant's credentials for this vendor from the vault (best-effort per
				// key; the responder validates which are required and reports a clear error if one
				// it needs is absent).
				masterKey, kerr := security.GetMasterKey()
				if kerr != nil {
					return fmt.Errorf("failed to retrieve encryption key: %w", kerr)
				}
				creds := make(map[string]string)
				for _, key := range responseVaultKeys[integrationType] {
					sec, gerr := vaultRepo.GetSecretByKey(ctx, tx, key)
					if gerr != nil {
						continue
					}
					decrypted, derr := security.DecryptForTenant(sec.EncryptedValue, sec.Nonce, masterKey, tenantID)
					if derr == nil {
						creds[key] = string(decrypted)
					}
				}

				log.Printf("[Response Action] Executing %s/%s on target %s (tenant %s)", integrationType, actionType, target, tenantID)
				out, xerr := resp.Execute(ctx, creds, responder.Action{Type: responder.ActionType(actionType), Target: target})
				if xerr != nil {
					finalStatus = "failed"
					output = fmt.Sprintf("Execution error: %v", xerr)
				} else {
					finalStatus = "approved"
					output = out
				}
			}

			if _, err := tx.Exec(ctx, `
				UPDATE response_action_requests
				SET status = $1, output = $2, approved_by = $3, approved_at = NOW()
				WHERE id = $4
			`, finalStatus, output, claims.UserID, req.RequestID); err != nil {
				return err
			}

			if incidentID != nil {
				icon := "🛡️"
				if finalStatus == "failed" {
					icon = "⚠️"
				}
				comment := fmt.Sprintf("%s **Ação de contenção [%s/%s] em %s**: %s\n\n%s", icon, integrationType, actionType, target, finalStatus, output)
				if _, err := tx.Exec(ctx, `
					INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
					VALUES ($1, $2, 'SOAR Response Engine', $3)
				`, *incidentID, tenantID, comment); err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to approve response action: %v", err), http.StatusInternalServerError)
			return
		}

		audit.Record(ctx, pgPool, audit.Entry{
			TenantID: tenantID, UserID: claims.UserID,
			Action:    "response.approve",
			Resource:  req.RequestID.String(),
			Details:   map[string]interface{}{"result": finalStatus},
			IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": finalStatus, "output": output})
	}
}

// HandleRejectResponseAction rejects a pending containment request without firing the vendor call.
func HandleRejectResponseAction(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized: User claims missing", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req ResponseActionDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			res, err := tx.Exec(ctx, `
				UPDATE response_action_requests
				SET status = 'rejected', approved_by = $1, approved_at = NOW()
				WHERE id = $2 AND tenant_id = $3 AND status = 'pending'
			`, claims.UserID, req.RequestID, tenantID)
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return fmt.Errorf("response action not found or no longer pending")
			}
			return nil
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to reject response action: %v", err), http.StatusInternalServerError)
			return
		}

		audit.Record(ctx, pgPool, audit.Entry{
			TenantID: tenantID, UserID: claims.UserID,
			Action:    "response.reject",
			Resource:  req.RequestID.String(),
			IPAddress: r.RemoteAddr,
		})

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","message":"Ação de contenção rejeitada"}`))
	}
}
