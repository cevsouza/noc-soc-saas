package api

import (
	"context"
	"fmt"

	"noc-api/internal/repository"
	"noc-api/internal/responder"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// executeResponderAction resolves the tenant's vendor credentials from the vault, runs a single
// containment action via the responder registry, and returns a final status ("approved" on success,
// "failed" otherwise) plus a human-readable output. It is the shared execution core behind both the
// single-action approval handler (HandleApproveResponseAction) and the multi-step playbook engine —
// keeping vault-credential resolution and vendor dispatch in exactly one place. A vendor/infra
// failure is reported as ("failed", message), never a panic/error, mirroring the "record what
// happened" convention of the containment queue. Must run inside the tenant's RLS transaction.
func executeResponderAction(ctx context.Context, tx pgx.Tx, vaultRepo repository.VaultRepository, tenantID uuid.UUID, integrationType, actionType, target string) (status, output string) {
	resp, ok := responder.Get(integrationType)
	if !ok {
		return "failed", fmt.Sprintf("no responder registered for integration %q", integrationType)
	}

	masterKey, kerr := security.GetMasterKey()
	if kerr != nil {
		return "failed", fmt.Sprintf("failed to retrieve encryption key: %v", kerr)
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

	out, xerr := resp.Execute(ctx, creds, responder.Action{Type: responder.ActionType(actionType), Target: target})
	if xerr != nil {
		return "failed", fmt.Sprintf("Execution error: %v", xerr)
	}
	return "approved", out
}
