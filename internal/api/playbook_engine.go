package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/model"
	"noc-api/internal/notifier"
	"noc-api/internal/playbook"
	"noc-api/internal/repository"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The playbook engine executes a playbook_run's steps in order. Side-effect-light steps (notify,
// comment) auto-execute; a response_action step mutates network/endpoint state and therefore PAUSES
// the run (status=awaiting_approval) until a human approves — the same human-in-the-loop guarantee
// the single-action containment queue enforces. Each step runs in its own tenant RLS transaction
// (vault read + outbound call + status write), mirroring HandleApproveResponseAction; a run holds no
// long-lived transaction across its HTTP calls.

// notifyVaultKeys maps a notify channel to the per-tenant vault secret holding its outbound
// credential (mirrors the worker's escalationVaultKeys).
var notifyVaultKeys = map[string]string{
	"slack":     "slack_webhook_url",
	"teams":     "teams_webhook_url",
	"email":     "email_recipient",
	"pagerduty": "pagerduty_routing_key",
	"opsgenie":  "opsgenie_api_key",
}

// stepRow is one materialized run step (its definition + current status).
type stepRow struct {
	id     uuid.UUID
	index  int
	step   playbook.Step
	status string
}

// runState is a run's execution snapshot loaded once for advancement.
type runState struct {
	runID       uuid.UUID
	incidentID  *uuid.UUID
	context     map[string]string
	currentStep int
	steps       []stepRow
}

// loadRunState reads a run and its steps under the tenant RLS context.
func loadRunState(ctx context.Context, pool *pgxpool.Pool, tenantID, runID uuid.UUID) (*runState, error) {
	rs := &runState{runID: runID, context: map[string]string{}}
	err := db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		var ctxBytes []byte
		if e := tx.QueryRow(ctx, `SELECT incident_id, current_step, context FROM playbook_runs WHERE id=$1 AND tenant_id=$2`,
			runID, tenantID).Scan(&rs.incidentID, &rs.currentStep, &ctxBytes); e != nil {
			return e
		}
		if len(ctxBytes) > 0 {
			_ = json.Unmarshal(ctxBytes, &rs.context)
		}
		rows, e := tx.Query(ctx, `SELECT id, step_index, params, status FROM playbook_run_steps WHERE run_id=$1 AND tenant_id=$2 ORDER BY step_index`, runID, tenantID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var sr stepRow
			var params []byte
			if e := rows.Scan(&sr.id, &sr.index, &params, &sr.status); e != nil {
				return e
			}
			_ = json.Unmarshal(params, &sr.step)
			rs.steps = append(rs.steps, sr)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return rs, nil
}

// startPlaybookRun loads a playbook, materializes a run + its steps, and advances it. Returns the run
// id and its resulting status (completed / awaiting_approval / failed).
func startPlaybookRun(ctx context.Context, pool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID, playbookID uuid.UUID, incidentID *uuid.UUID, runCtx map[string]string, startedBy string) (uuid.UUID, string, error) {
	var steps []playbook.Step
	var runID uuid.UUID

	err := db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		var stepsBytes []byte
		var enabled bool
		if e := tx.QueryRow(ctx, `SELECT steps, enabled FROM playbooks WHERE id=$1 AND tenant_id=$2`, playbookID, tenantID).Scan(&stepsBytes, &enabled); e != nil {
			return fmt.Errorf("playbook not found: %w", e)
		}
		if !enabled {
			return fmt.Errorf("playbook is disabled")
		}
		if e := json.Unmarshal(stepsBytes, &steps); e != nil {
			return fmt.Errorf("invalid playbook steps: %w", e)
		}
		if e := playbook.ValidateSteps(steps); e != nil {
			return e
		}
		ctxBytes, _ := json.Marshal(runCtx)
		if e := tx.QueryRow(ctx, `
			INSERT INTO playbook_runs (tenant_id, playbook_id, incident_id, status, current_step, context, started_by)
			VALUES ($1,$2,$3,'running',0,$4,$5) RETURNING id
		`, tenantID, playbookID, incidentID, ctxBytes, startedBy).Scan(&runID); e != nil {
			return e
		}
		for i, s := range steps {
			params, _ := json.Marshal(s)
			if _, e := tx.Exec(ctx, `
				INSERT INTO playbook_run_steps (run_id, tenant_id, step_index, step_type, params, status)
				VALUES ($1,$2,$3,$4,$5,'pending')
			`, runID, tenantID, i, s.Type, params); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, "", err
	}

	status, err := advancePlaybookRun(ctx, pool, vaultRepo, tenantID, runID)
	return runID, status, err
}

// advancePlaybookRun executes steps from the run's current_step until it reaches a response_action
// (pauses for approval), a response_action failure (fails the run), or the end (completes).
func advancePlaybookRun(ctx context.Context, pool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID, runID uuid.UUID) (string, error) {
	rs, err := loadRunState(ctx, pool, tenantID, runID)
	if err != nil {
		return "", err
	}

	for i := rs.currentStep; i < len(rs.steps); i++ {
		sr := rs.steps[i]
		if playbook.StepNeedsApproval(sr.step.Type) {
			// Pause: mark the step awaiting_approval (recording the resolved target for the reviewer)
			// and the run awaiting_approval at this index.
			target, terr := playbook.ResolveTarget(sr.step, rs.context)
			note := fmt.Sprintf("Aguardando aprovação: %s/%s em %s", sr.step.IntegrationType, sr.step.ActionType, target)
			if terr != nil {
				note = fmt.Sprintf("Aguardando aprovação: %s/%s (alvo não resolvido: %v)", sr.step.IntegrationType, sr.step.ActionType, terr)
			}
			if e := setRunPaused(ctx, pool, tenantID, runID, sr.id, i, note); e != nil {
				return "", e
			}
			return "awaiting_approval", nil
		}
		// Auto step (notify/comment): best-effort, run continues even on failure.
		status, out := executeAutoStep(ctx, pool, vaultRepo, tenantID, rs, sr.step)
		if e := updateStep(ctx, pool, tenantID, sr.id, status, out); e != nil {
			return "", e
		}
	}

	if e := setRunStatus(ctx, pool, tenantID, runID, "completed", len(rs.steps)); e != nil {
		return "", e
	}
	return "completed", nil
}

// approvePlaybookStep executes the paused response_action step, then resumes advancement. A failed
// action fails the run; a successful one moves to the next step.
func approvePlaybookStep(ctx context.Context, pool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID, runID uuid.UUID, approverID uuid.UUID) (string, error) {
	rs, err := loadRunState(ctx, pool, tenantID, runID)
	if err != nil {
		return "", err
	}
	if rs.currentStep >= len(rs.steps) {
		return "", fmt.Errorf("run has no pending step")
	}
	sr := rs.steps[rs.currentStep]
	if !playbook.StepNeedsApproval(sr.step.Type) || sr.status != "awaiting_approval" {
		return "", fmt.Errorf("current step is not awaiting approval")
	}

	target, terr := playbook.ResolveTarget(sr.step, rs.context)
	if terr != nil {
		_ = updateStep(ctx, pool, tenantID, sr.id, "failed", terr.Error())
		_ = setRunStatus(ctx, pool, tenantID, runID, "failed", rs.currentStep)
		return "failed", nil
	}

	var status, output string
	err = db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		log.Printf("[Playbook] Executing response_action %s/%s on %s (run %s)", sr.step.IntegrationType, sr.step.ActionType, target, runID)
		status, output = executeResponderAction(ctx, tx, vaultRepo, tenantID, sr.step.IntegrationType, sr.step.ActionType, target)
		stepStatus := "succeeded"
		if status != "approved" {
			stepStatus = "failed"
		}
		if _, e := tx.Exec(ctx, `UPDATE playbook_run_steps SET status=$1, output=$2, updated_at=NOW() WHERE id=$3`, stepStatus, output, sr.id); e != nil {
			return e
		}
		if rs.incidentID != nil {
			icon := "🛡️"
			if stepStatus == "failed" {
				icon = "⚠️"
			}
			comment := fmt.Sprintf("%s **Playbook — contenção [%s/%s] em %s**: %s\n\n%s", icon, sr.step.IntegrationType, sr.step.ActionType, target, stepStatus, output)
			if _, e := tx.Exec(ctx, `INSERT INTO incident_comments (incident_id, tenant_id, author, comment) VALUES ($1,$2,'SOAR Playbook Engine',$3)`, *rs.incidentID, tenantID, comment); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	_ = approverID // recorded via audit at the handler layer

	if status != "approved" {
		if e := setRunStatus(ctx, pool, tenantID, runID, "failed", rs.currentStep); e != nil {
			return "", e
		}
		return "failed", nil
	}
	// Move past the approved step and continue.
	if e := setRunStatus(ctx, pool, tenantID, runID, "running", rs.currentStep+1); e != nil {
		return "", e
	}
	return advancePlaybookRun(ctx, pool, vaultRepo, tenantID, runID)
}

// rejectPlaybookRun aborts a run awaiting approval without firing the pending action.
func rejectPlaybookRun(ctx context.Context, pool *pgxpool.Pool, tenantID, runID uuid.UUID) error {
	return db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		ct, e := tx.Exec(ctx, `UPDATE playbook_runs SET status='rejected', updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND status='awaiting_approval'`, runID, tenantID)
		if e != nil {
			return e
		}
		if ct.RowsAffected() == 0 {
			return fmt.Errorf("run is not awaiting approval")
		}
		_, _ = tx.Exec(ctx, `UPDATE playbook_run_steps SET status='skipped', output='rejeitado pelo operador', updated_at=NOW() WHERE run_id=$1 AND tenant_id=$2 AND status='awaiting_approval'`, runID, tenantID)
		return nil
	})
}

// executeAutoStep runs a notify or comment step (best-effort). Returns (stepStatus, output).
func executeAutoStep(ctx context.Context, pool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID uuid.UUID, rs *runState, s playbook.Step) (string, string) {
	switch s.Type {
	case playbook.StepComment:
		if rs.incidentID == nil {
			return "skipped", "nenhum incidente vinculado ao run; comentário ignorado"
		}
		err := db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO incident_comments (incident_id, tenant_id, author, comment) VALUES ($1,$2,'SOAR Playbook Engine',$3)`, *rs.incidentID, tenantID, s.Text)
			return e
		})
		if err != nil {
			return "failed", err.Error()
		}
		return "succeeded", "comentário publicado"
	case playbook.StepNotify:
		out, err := sendPlaybookNotify(ctx, pool, vaultRepo, tenantID, rs, s)
		if err != nil {
			return "failed", err.Error()
		}
		return "succeeded", out
	default:
		return "failed", fmt.Sprintf("tipo de passo não executável: %s", s.Type)
	}
}

// sendPlaybookNotify resolves the channel's vault secret and pages it with a synthetic alert built
// from the run's incident (title/severity) plus the step's optional message override.
func sendPlaybookNotify(ctx context.Context, pool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID uuid.UUID, rs *runState, s playbook.Step) (string, error) {
	key, ok := notifyVaultKeys[s.Channel]
	if !ok {
		return "", fmt.Errorf("canal desconhecido %q", s.Channel)
	}
	// Build the alert to page from the bound incident (falls back to the step message).
	alert := &model.Alert{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Severity:  model.SeverityCritical,
		Status:    model.AlertTriggered,
		EventType: "playbook",
		Summary:   s.Message,
		CreatedAt: time.Now(),
	}
	if rs.incidentID != nil {
		_ = db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
			var title, sev string
			if e := tx.QueryRow(ctx, `SELECT title, severity FROM incidents WHERE id=$1 AND tenant_id=$2`, *rs.incidentID, tenantID).Scan(&title, &sev); e == nil {
				if alert.Summary == "" {
					alert.Summary = title
				}
				alert.Severity = model.AlertSeverity(sev)
			}
			return nil
		})
	}
	if alert.Summary == "" {
		alert.Summary = "Playbook SOAR acionado"
	}

	var secret string
	if e := db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		masterKey, kerr := security.GetMasterKey()
		if kerr != nil {
			return kerr
		}
		sec, gerr := vaultRepo.GetSecretByKey(ctx, tx, key)
		if gerr != nil {
			return fmt.Errorf("segredo %s ausente: %w", key, gerr)
		}
		dec, derr := security.DecryptForTenant(sec.EncryptedValue, sec.Nonce, masterKey, tenantID)
		if derr != nil {
			return derr
		}
		secret = string(dec)
		return nil
	}); e != nil {
		return "", e
	}

	var nerr error
	switch s.Channel {
	case "slack":
		nerr = notifier.NewSlackNotifier().Notify(ctx, secret, alert)
	case "teams":
		nerr = notifier.NewTeamsNotifier().Notify(ctx, secret, alert)
	case "email":
		nerr = notifier.NewEmailNotifier().Notify(ctx, secret, alert)
	case "pagerduty":
		nerr = notifier.NewPagerDutyNotifier().Notify(ctx, secret, alert)
	case "opsgenie":
		nerr = notifier.NewOpsgenieNotifier().Notify(ctx, secret, alert)
	}
	if nerr != nil {
		return "", nerr
	}
	return "notificação enviada para " + s.Channel, nil
}

// --- small persistence helpers ---

func updateStep(ctx context.Context, pool *pgxpool.Pool, tenantID, stepID uuid.UUID, status, output string) error {
	return db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE playbook_run_steps SET status=$1, output=$2, updated_at=NOW() WHERE id=$3 AND tenant_id=$4`, status, output, stepID, tenantID)
		return e
	})
}

func setRunStatus(ctx context.Context, pool *pgxpool.Pool, tenantID, runID uuid.UUID, status string, currentStep int) error {
	return db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE playbook_runs SET status=$1, current_step=$2, updated_at=NOW() WHERE id=$3 AND tenant_id=$4`, status, currentStep, runID, tenantID)
		return e
	})
}

func setRunPaused(ctx context.Context, pool *pgxpool.Pool, tenantID, runID, stepID uuid.UUID, stepIndex int, note string) error {
	return db.ExecuteInTenantTx(ctx, pool, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE playbook_run_steps SET status='awaiting_approval', output=$1, updated_at=NOW() WHERE id=$2 AND tenant_id=$3`, note, stepID, tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `UPDATE playbook_runs SET status='awaiting_approval', current_step=$1, updated_at=NOW() WHERE id=$2 AND tenant_id=$3`, stepIndex, runID, tenantID)
		return e
	})
}
