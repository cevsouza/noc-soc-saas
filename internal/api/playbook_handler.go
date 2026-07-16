package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"noc-api/internal/playbook"
	"noc-api/internal/repository"
	"noc-api/internal/responder"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// writeJSON writes v as a JSON response with the right content type.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// PlaybookDef is a playbook definition as returned by the API.
type PlaybookDef struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Steps       []playbook.Step `json:"steps"`
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
}

// CreatePlaybookRequest is the body of POST /api/v1/playbooks.
type CreatePlaybookRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Steps       []playbook.Step `json:"steps"`
}

// validatePlaybookSteps runs the pure structural check plus a semantic check that each
// response_action step names a vendor/action the live responder registry actually supports.
func validatePlaybookSteps(steps []playbook.Step) error {
	if err := playbook.ValidateSteps(steps); err != nil {
		return err
	}
	for i, s := range steps {
		if s.Type == playbook.StepResponseAction {
			if !responder.Supports(s.IntegrationType, responder.ActionType(s.ActionType)) {
				return fmt.Errorf("step %d: integration %q does not support action %q", i, s.IntegrationType, s.ActionType)
			}
		}
	}
	return nil
}

// HandlePlaybooks lists (GET), creates (POST) and deletes (DELETE ?id=) playbook definitions on a
// single path (method dispatch, no ServeMux collisions). Mutations require tenant_admin; listing is
// open to any authenticated tenant member.
func HandlePlaybooks(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)
		isAdmin := model.TenantRoleAtLeast(claims.Role, model.RoleTenantAdmin) || model.IsPlatformAdmin(claims.GlobalRole)

		switch r.Method {
		case http.MethodGet:
			var out []PlaybookDef
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				rows, e := tx.Query(ctx, `SELECT id, name, description, steps, enabled, created_at FROM playbooks WHERE tenant_id=$1 ORDER BY name`, tenantID)
				if e != nil {
					return e
				}
				defer rows.Close()
				for rows.Next() {
					var p PlaybookDef
					var stepsB []byte
					if e := rows.Scan(&p.ID, &p.Name, &p.Description, &stepsB, &p.Enabled, &p.CreatedAt); e != nil {
						return e
					}
					_ = json.Unmarshal(stepsB, &p.Steps)
					out = append(out, p)
				}
				return rows.Err()
			})
			if err != nil {
				http.Error(w, "failed to list playbooks", http.StatusInternalServerError)
				return
			}
			writeJSON(w, out)

		case http.MethodPost:
			if !isAdmin {
				http.Error(w, "forbidden: tenant_admin required", http.StatusForbidden)
				return
			}
			var req CreatePlaybookRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			req.Name = strings.TrimSpace(req.Name)
			if req.Name == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}
			if verr := validatePlaybookSteps(req.Steps); verr != nil {
				http.Error(w, verr.Error(), http.StatusBadRequest)
				return
			}
			stepsB, _ := json.Marshal(req.Steps)
			var newID uuid.UUID
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `
					INSERT INTO playbooks (tenant_id, name, description, steps, enabled)
					VALUES ($1,$2,$3,$4,TRUE)
					ON CONFLICT (tenant_id, name) DO UPDATE SET description=EXCLUDED.description, steps=EXCLUDED.steps, updated_at=NOW()
					RETURNING id
				`, tenantID, req.Name, req.Description, stepsB).Scan(&newID)
			})
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to save playbook: %v", err), http.StatusInternalServerError)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: claims.UserID, Action: "playbook.save", Resource: newID.String(), IPAddress: r.RemoteAddr})
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]string{"id": newID.String()})

		case http.MethodDelete:
			if !isAdmin {
				http.Error(w, "forbidden: tenant_admin required", http.StatusForbidden)
				return
			}
			id := strings.TrimSpace(r.URL.Query().Get("id"))
			pid, perr := uuid.Parse(id)
			if perr != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `DELETE FROM playbooks WHERE id=$1 AND tenant_id=$2`, pid, tenantID)
				return e
			})
			if err != nil {
				http.Error(w, "failed to delete", http.StatusInternalServerError)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: claims.UserID, Action: "playbook.delete", Resource: pid.String(), IPAddress: r.RemoteAddr})
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// RunPlaybookRequest is the body of POST /api/v1/playbooks/run.
type RunPlaybookRequest struct {
	PlaybookID string            `json:"playbook_id"`
	IncidentID *string           `json:"incident_id,omitempty"`
	Context    map[string]string `json:"context,omitempty"`
}

// HandleRunPlaybook starts a run of a playbook for a tenant, executing auto steps and pausing at the
// first response_action for approval. Returns the run id and resulting status.
func HandleRunPlaybook(pgPool *pgxpool.Pool) http.HandlerFunc {
	vaultRepo := repository.NewPostgresVaultRepository()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req RunPlaybookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		pid, perr := uuid.Parse(strings.TrimSpace(req.PlaybookID))
		if perr != nil {
			http.Error(w, "invalid playbook_id", http.StatusBadRequest)
			return
		}
		var incidentID *uuid.UUID
		if req.IncidentID != nil && strings.TrimSpace(*req.IncidentID) != "" {
			parsed, e := uuid.Parse(strings.TrimSpace(*req.IncidentID))
			if e != nil {
				http.Error(w, "invalid incident_id", http.StatusBadRequest)
				return
			}
			incidentID = &parsed
		}
		startedBy := claims.Email
		if startedBy == "" {
			startedBy = claims.UserID.String()
		}

		runID, status, rerr := startPlaybookRun(ctx, pgPool, vaultRepo, tenantID, pid, incidentID, req.Context, startedBy)
		if rerr != nil {
			http.Error(w, fmt.Sprintf("failed to start playbook: %v", rerr), http.StatusBadRequest)
			return
		}
		audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: claims.UserID, Action: "playbook.run", Resource: runID.String(), Details: map[string]interface{}{"status": status}, IPAddress: r.RemoteAddr})
		writeJSON(w, map[string]string{"run_id": runID.String(), "status": status})
	}
}

// PlaybookRunStepView is one step in a run detail.
type PlaybookRunStepView struct {
	StepIndex int    `json:"step_index"`
	StepType  string `json:"step_type"`
	Status    string `json:"status"`
	Output    string `json:"output"`
}

// PlaybookRunView is a run row (with steps when a single run is requested by ?id=).
type PlaybookRunView struct {
	ID          uuid.UUID             `json:"id"`
	PlaybookID  uuid.UUID             `json:"playbook_id"`
	IncidentID  *uuid.UUID            `json:"incident_id,omitempty"`
	Status      string                `json:"status"`
	CurrentStep int                   `json:"current_step"`
	StartedBy   string                `json:"started_by"`
	CreatedAt   time.Time             `json:"created_at"`
	Steps       []PlaybookRunStepView `json:"steps,omitempty"`
}

// HandleGetPlaybookRuns lists runs for the tenant, or returns one run with its steps when ?id= is set.
func HandleGetPlaybookRuns(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)
		singleID := strings.TrimSpace(r.URL.Query().Get("id"))

		var out []PlaybookRunView
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			q := `SELECT id, playbook_id, incident_id, status, current_step, started_by, created_at FROM playbook_runs WHERE tenant_id=$1`
			args := []interface{}{tenantID}
			if singleID != "" {
				q += ` AND id=$2`
				args = append(args, singleID)
			}
			q += ` ORDER BY created_at DESC LIMIT 100`
			rows, e := tx.Query(ctx, q, args...)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var v PlaybookRunView
				if e := rows.Scan(&v.ID, &v.PlaybookID, &v.IncidentID, &v.Status, &v.CurrentStep, &v.StartedBy, &v.CreatedAt); e != nil {
					return e
				}
				out = append(out, v)
			}
			if e := rows.Err(); e != nil {
				return e
			}
			// Attach steps only when a single run was requested.
			if singleID != "" && len(out) == 1 {
				srows, e := tx.Query(ctx, `SELECT step_index, step_type, status, output FROM playbook_run_steps WHERE run_id=$1 AND tenant_id=$2 ORDER BY step_index`, out[0].ID, tenantID)
				if e != nil {
					return e
				}
				defer srows.Close()
				for srows.Next() {
					var s PlaybookRunStepView
					if e := srows.Scan(&s.StepIndex, &s.StepType, &s.Status, &s.Output); e != nil {
						return e
					}
					out[0].Steps = append(out[0].Steps, s)
				}
				return srows.Err()
			}
			return nil
		})
		if err != nil {
			http.Error(w, "failed to list runs", http.StatusInternalServerError)
			return
		}
		writeJSON(w, out)
	}
}

// PlaybookRunDecisionRequest is the body of the approve/reject endpoints.
type PlaybookRunDecisionRequest struct {
	RunID string `json:"run_id"`
}

// HandleApprovePlaybookRun approves the paused response_action step and resumes the run.
func HandleApprovePlaybookRun(pgPool *pgxpool.Pool) http.HandlerFunc {
	vaultRepo := repository.NewPostgresVaultRepository()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req PlaybookRunDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		runID, perr := uuid.Parse(strings.TrimSpace(req.RunID))
		if perr != nil {
			http.Error(w, "invalid run_id", http.StatusBadRequest)
			return
		}

		status, aerr := approvePlaybookStep(ctx, pgPool, vaultRepo, tenantID, runID, claims.UserID)
		if aerr != nil {
			http.Error(w, fmt.Sprintf("failed to approve: %v", aerr), http.StatusBadRequest)
			return
		}
		audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: claims.UserID, Action: "playbook.approve", Resource: runID.String(), Details: map[string]interface{}{"status": status}, IPAddress: r.RemoteAddr})
		writeJSON(w, map[string]string{"status": status})
	}
}

// HandleRejectPlaybookRun aborts a run awaiting approval.
func HandleRejectPlaybookRun(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req PlaybookRunDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		runID, perr := uuid.Parse(strings.TrimSpace(req.RunID))
		if perr != nil {
			http.Error(w, "invalid run_id", http.StatusBadRequest)
			return
		}
		if rerr := rejectPlaybookRun(ctx, pgPool, tenantID, runID); rerr != nil {
			http.Error(w, fmt.Sprintf("failed to reject: %v", rerr), http.StatusBadRequest)
			return
		}
		audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: claims.UserID, Action: "playbook.reject", Resource: runID.String(), IPAddress: r.RemoteAddr})
		writeJSON(w, map[string]string{"status": "rejected"})
	}
}
