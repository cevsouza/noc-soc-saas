// Package audit centralizes append-only audit logging of sensitive actions (approvals, runbook
// execution, secret writes, containment). Records land in the audit_logs table, which a database
// trigger + REVOKE make write-once for the application role (see migration 000017): once written,
// the running app can never edit or delete them.
package audit

import (
	"context"
	"encoding/json"
	"log"

	"noc-api/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry is one audit record. Details is free-form structured context (never secrets in cleartext).
type Entry struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	Action    string // e.g. "runbook.approve", "response.reject", "vault.secret.save"
	Resource  string // the acted-on object, e.g. an approval id, integration type, runbook name
	Details   map[string]interface{}
	IPAddress string
}

// marshalArgs prepares the JSON details blob and nullable IP for the INSERT. Pure and testable.
func marshalArgs(e Entry) (detailsJSON []byte, ip *string, err error) {
	d := e.Details
	if d == nil {
		d = map[string]interface{}{}
	}
	detailsJSON, err = json.Marshal(d)
	if err != nil {
		return nil, nil, err
	}
	if e.IPAddress != "" {
		ip = &e.IPAddress
	}
	return detailsJSON, ip, nil
}

// Record writes the audit row inside the entry's tenant RLS context. It is best-effort by design:
// failing to audit must never fail or roll back the user's action, so errors are logged, not
// returned. Call it after the action has already succeeded.
func Record(ctx context.Context, pool *pgxpool.Pool, e Entry) {
	detailsJSON, ip, err := marshalArgs(e)
	if err != nil {
		log.Printf("[Audit] Failed to marshal details for %s on %s: %v", e.Action, e.Resource, err)
		return
	}
	// user_id is nullable (FK to users ON DELETE SET NULL); a zero UUID means "no user" (a
	// system-initiated action), so insert NULL rather than a non-existent FK value.
	var userID interface{}
	if e.UserID != uuid.Nil {
		userID = e.UserID
	}
	tctx := db.WithTenantID(ctx, e.TenantID)
	err = db.ExecuteInTenantTx(tctx, pool, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(tctx, `
			INSERT INTO audit_logs (tenant_id, user_id, action, resource, details, ip_address)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, e.TenantID, userID, e.Action, e.Resource, detailsJSON, ip)
		return execErr
	})
	if err != nil {
		log.Printf("[Audit] Failed to record %s on %s (tenant %s): %v", e.Action, e.Resource, e.TenantID, err)
	}
}
