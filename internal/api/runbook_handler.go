package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	"noc-api/internal/repository"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/ssh"
)

type CreateRunbookRequest struct {
	Name         string `json:"name"`
	TriggerRule  string `json:"trigger_rule"`
	Script       string `json:"script"`
	VaultKeyHost string `json:"vault_key_host"`
	IsGlobal     bool   `json:"is_global"`
	// IsSafe marks the runbook as reviewed/idempotent enough to auto-execute without human
	// approval when auto_trigger is set. Defaults to false (secure by default) — an admin
	// must explicitly opt a runbook in after reviewing it.
	IsSafe bool `json:"is_safe"`
}

type ExecuteRunbookRequest struct {
	RunbookID  uuid.UUID `json:"runbook_id"`
	IncidentID uuid.UUID `json:"incident_id"`
}

// HandleGetRunbooks returns the list of runbooks. If tenant_id = all, it bypasses RLS and returns all runbooks.
func HandleGetRunbooks(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type RunbookResponse struct {
			ID           uuid.UUID `json:"id"`
			TenantID     uuid.UUID `json:"tenant_id"`
			TenantName   string    `json:"tenant_name"`
			Name         string    `json:"name"`
			TriggerRule  string    `json:"trigger_rule"`
			Script       string    `json:"script"`
			VaultKeyHost string    `json:"vault_key_host"`
			IsGlobal     bool      `json:"is_global"`
			IsSafe       bool      `json:"is_safe"`
			CreatedAt    time.Time `json:"created_at"`
		}

		list := make([]RunbookResponse, 0)

		allScope, err := middleware.AllTenantsScope(r.Context(), r)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}

		if allScope {
			tx, err := pgPool.Begin(r.Context())
			if err != nil {
				http.Error(w, "Failed to begin transaction", http.StatusInternalServerError)
				return
			}
			defer tx.Rollback(r.Context())

			if _, err := tx.Exec(r.Context(), "SET LOCAL app.bypass_rls = 'true'"); err != nil {
				http.Error(w, "Failed to bypass RLS", http.StatusInternalServerError)
				return
			}

			rows, err := tx.Query(r.Context(), `
				SELECT r.id, r.tenant_id, t.name, r.name, r.trigger_rule, r.script, r.vault_key_host, r.is_global, r.is_safe, r.created_at
				FROM tenant_runbooks r
				JOIN tenants t ON r.tenant_id = t.id
				ORDER BY r.name
			`)
			if err != nil {
				http.Error(w, "Failed to query all runbooks", http.StatusInternalServerError)
				return
			}
			defer rows.Close()

			for rows.Next() {
				var rb RunbookResponse
				err := rows.Scan(&rb.ID, &rb.TenantID, &rb.TenantName, &rb.Name, &rb.TriggerRule, &rb.Script, &rb.VaultKeyHost, &rb.IsGlobal, &rb.IsSafe, &rb.CreatedAt)
				if err != nil {
					http.Error(w, "Error scanning runbooks", http.StatusInternalServerError)
					return
				}
				list = append(list, rb)
			}
			if err := tx.Commit(r.Context()); err != nil {
				http.Error(w, "Failed to commit transaction", http.StatusInternalServerError)
				return
			}
		} else {
			tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
			if err != nil {
				middleware.WriteScopeError(w, err)
				return
			}
			ctx := db.WithTenantID(r.Context(), tenantID)

			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				rows, err := tx.Query(ctx, `
					SELECT r.id, r.tenant_id, t.name, r.name, r.trigger_rule, r.script, r.vault_key_host, r.is_global, r.is_safe, r.created_at
					FROM tenant_runbooks r
					JOIN tenants t ON r.tenant_id = t.id
					WHERE r.tenant_id = $1 OR r.is_global = TRUE
					ORDER BY r.name
				`, tenantID)
				if err != nil {
					return err
				}
				defer rows.Close()

				for rows.Next() {
					var rb RunbookResponse
					err := rows.Scan(&rb.ID, &rb.TenantID, &rb.TenantName, &rb.Name, &rb.TriggerRule, &rb.Script, &rb.VaultKeyHost, &rb.IsGlobal, &rb.IsSafe, &rb.CreatedAt)
					if err != nil {
						return err
					}
					list = append(list, rb)
				}
				return rows.Err()
			})

			if err != nil {
				http.Error(w, "Failed to query runbooks", http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleCreateRunbook registers a new runbook for the tenant.
func HandleCreateRunbook(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req CreateRunbookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		if req.Name == "" || req.Script == "" || req.VaultKeyHost == "" {
			http.Error(w, "Missing required fields (name, script, vault_key_host)", http.StatusBadRequest)
			return
		}

		var runbookID uuid.UUID
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := "INSERT INTO tenant_runbooks (tenant_id, name, trigger_rule, script, vault_key_host, is_global, is_safe) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id"
			return tx.QueryRow(ctx, query, tenantID, req.Name, req.TriggerRule, req.Script, req.VaultKeyHost, req.IsGlobal, req.IsSafe).Scan(&runbookID)
		})

		if err != nil {
			http.Error(w, "Failed to insert runbook", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": runbookID, "message": "Runbook criado com sucesso"})
	}
}

// HandleExecuteRunbook executes the runbook script on the remote host over SSH using credentials from the vault.
func HandleExecuteRunbook(pgPool *pgxpool.Pool) http.HandlerFunc {
	vaultRepo := repository.NewPostgresVaultRepository()

	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var req ExecuteRunbookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		var name, script, vaultKeyPrefix string
		var execStatus, output string

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			// 1. Fetch Runbook details
			err := tx.QueryRow(ctx, "SELECT name, script, vault_key_host FROM tenant_runbooks WHERE id = $1 AND (tenant_id = $2 OR is_global = TRUE)", req.RunbookID, tenantID).Scan(&name, &script, &vaultKeyPrefix)
			if err != nil {
				return err
			}

			// 2. Fetch connection credentials from Vault
			masterKey, err := security.GetMasterKey()
			if err != nil {
				return fmt.Errorf("failed to retrieve encryption key: %w", err)
			}

			// Helper to fetch and decrypt a secret key
			getSecret := func(key string) string {
				sec, err := vaultRepo.GetSecretByKey(ctx, tx, key)
				if err != nil {
					return ""
				}
				decrypted, err := security.Decrypt(sec.EncryptedValue, sec.Nonce, masterKey)
				if err != nil {
					return ""
				}
				return string(decrypted)
			}

			sshHost := getSecret(fmt.Sprintf("%s_host", vaultKeyPrefix))
			sshUser := getSecret(fmt.Sprintf("%s_user", vaultKeyPrefix))
			sshPass := getSecret(fmt.Sprintf("%s_password", vaultKeyPrefix))
			sshPriv := getSecret(fmt.Sprintf("%s_private_key", vaultKeyPrefix))

			if sshHost == "" || sshUser == "" {
				return fmt.Errorf("missing host or user secret in Vault under prefix: %s", vaultKeyPrefix)
			}

			// Log connection attempt
			log.Printf("[Runbook] Executing runbook '%s' on %s@%s", name, sshUser, sshHost)

			// 3. Execute SSH command
			output, err = ExecuteSSH(ctx, pgPool, vaultRepo, tenantID, vaultKeyPrefix, sshHost, sshUser, sshPass, sshPriv, script)
			execStatus = "sucesso"
			if err != nil {
				execStatus = "falha"
				output = fmt.Sprintf("Execution Error: %v\nLogs:\n%s", err, output)
			}

			// 4. Record execution log in incident comments/history
			logQuery := `
				INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
				VALUES ($1, $2, 'SRE Auto-Healing Co-Pilot', $3)
			`
			commentText := fmt.Sprintf("🤖 **Execução de Runbook [%s]**: Status: %s\n\n```bash\n%s\n```", name, execStatus, output)
			_, err = tx.Exec(ctx, logQuery, req.IncidentID, tenantID, commentText)
			if err != nil {
				return err
			}

			// 5. Record log in the execution audit logs table for security compliance
			auditQuery := `
				INSERT INTO runbook_execution_logs (tenant_id, runbook_id, incident_id, operator_name, script, output, status)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`
			_, err = tx.Exec(ctx, auditQuery, tenantID, req.RunbookID, req.IncidentID, "SRE Operador Co-Pilot", script, output, execStatus)
			return err
		})

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "Runbook not found", http.StatusNotFound)
			} else {
				http.Error(w, fmt.Sprintf("Execution or database error: %v", err), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": execStatus,
			"output": output,
		})
	}
}

// ExecuteSSH executes script commands on a remote host, pinning the host key on first use
// (Trust On First Use) and validating it on every subsequent connection to detect MITM or
// unexpected host reconfiguration. The fingerprint is stored per-tenant, per-runbook-host in
// the same encrypted vault used for credentials (secret key: "<vaultKeyPrefix>_known_host_fp").
func ExecuteSSH(ctx context.Context, pgPool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID uuid.UUID, vaultKeyPrefix, host, username, password, privateKey, command string) (string, error) {
	var authMethods []ssh.AuthMethod

	if privateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(privateKey))
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	if len(authMethods) == 0 {
		return "", fmt.Errorf("no valid SSH authentication method found in Vault credentials")
	}

	hostKeyCallback, err := buildTOFUHostKeyCallback(ctx, pgPool, vaultRepo, tenantID, vaultKeyPrefix)
	if err != nil {
		return "", fmt.Errorf("failed to prepare host key verification: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return "", fmt.Errorf("failed to dial SSH remote server: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to open SSH session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	err = session.Run(command)
	output := stdoutBuf.String()
	if stderrBuf.Len() > 0 {
		output += "\n[Stderr Output]\n" + stderrBuf.String()
	}

	if err != nil {
		return output, fmt.Errorf("execution error: %w", err)
	}

	return output, nil
}

// buildTOFUHostKeyCallback implements Trust On First Use host key verification: the first
// connection to a given (tenant, vaultKeyPrefix) pins the host's SHA256 fingerprint in the
// vault; every later connection must match it exactly, or the dial is rejected — protecting
// against MITM or an unexpected host swap going unnoticed.
func buildTOFUHostKeyCallback(ctx context.Context, pgPool *pgxpool.Pool, vaultRepo repository.VaultRepository, tenantID uuid.UUID, vaultKeyPrefix string) (ssh.HostKeyCallback, error) {
	fingerprintKey := vaultKeyPrefix + "_known_host_fp"

	masterKey, err := security.GetMasterKey()
	if err != nil {
		return nil, err
	}

	tenantCtx := db.WithTenantID(ctx, tenantID)
	var storedFP string
	_ = db.ExecuteInTenantTx(tenantCtx, pgPool, func(tx pgx.Tx) error {
		sec, err := vaultRepo.GetSecretByKey(tenantCtx, tx, fingerprintKey)
		if err != nil {
			return nil // Not pinned yet: first connection, handled below.
		}
		decrypted, err := security.Decrypt(sec.EncryptedValue, sec.Nonce, masterKey)
		if err == nil {
			storedFP = string(decrypted)
		}
		return nil
	})

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)

		if storedFP == "" {
			// Trust On First Use: persist the fingerprint for future connections.
			encrypted, nonce, err := security.Encrypt([]byte(fp), masterKey)
			if err != nil {
				return fmt.Errorf("TOFU: failed to encrypt host fingerprint: %w", err)
			}
			secret := &model.VaultSecret{
				ID:             uuid.New(),
				TenantID:       tenantID,
				SecretKey:      fingerprintKey,
				EncryptedValue: encrypted,
				Nonce:          nonce,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			pinErr := db.ExecuteInTenantTx(tenantCtx, pgPool, func(tx pgx.Tx) error {
				return vaultRepo.CreateSecret(tenantCtx, tx, secret)
			})
			if pinErr != nil {
				log.Printf("[SSH TOFU] Warning: failed to persist host fingerprint for %s (%s): %v", hostname, vaultKeyPrefix, pinErr)
			} else {
				log.Printf("[SSH TOFU] Pinned new host key fingerprint for %s (%s): %s", hostname, vaultKeyPrefix, fp)
			}
			return nil
		}

		if fp != storedFP {
			return fmt.Errorf("SSH host key mismatch for %s: expected %s, got %s (possible MITM or host reconfiguration — to accept the new key, delete vault secret %q for this tenant)", hostname, storedFP, fp, fingerprintKey)
		}
		return nil
	}, nil
}

type RunbookAuditResponse struct {
	ID           uuid.UUID `json:"id"`
	RunbookName  string    `json:"runbook_name"`
	OperatorName string    `json:"operator_name"`
	Script       string    `json:"script"`
	Output       string    `json:"output"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// HandleGetRunbookAuditLogs returns the list of runbook executions for security audit trail.
func HandleGetRunbookAuditLogs(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]RunbookAuditResponse, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := `
				SELECT l.id, r.name, l.operator_name, l.script, l.output, l.status, l.created_at
				FROM runbook_execution_logs l
				JOIN tenant_runbooks r ON l.runbook_id = r.id
				WHERE l.tenant_id = $1
				ORDER BY l.created_at DESC
				LIMIT 50
			`
			rows, err := tx.Query(ctx, query, tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var item RunbookAuditResponse
				err := rows.Scan(&item.ID, &item.RunbookName, &item.OperatorName, &item.Script, &item.Output, &item.Status, &item.CreatedAt)
				if err != nil {
					return err
				}
				list = append(list, item)
			}
			return rows.Err()
		})

		if err != nil {
			http.Error(w, "Failed to query runbook audits", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleDeleteRunbook removes a runbook configuration.
func HandleDeleteRunbook(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		idStr := r.URL.Query().Get("id")
		if idStr == "" {
			http.Error(w, "Missing runbook ID parameter", http.StatusBadRequest)
			return
		}

		runbookID, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "Invalid ID format", http.StatusBadRequest)
			return
		}

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			res, err := tx.Exec(ctx, "DELETE FROM tenant_runbooks WHERE id = $1 AND tenant_id = $2", runbookID, tenantID)
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return pgx.ErrNoRows
			}
			return nil
		})

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				http.Error(w, "Runbook not found or unauthorized", http.StatusNotFound)
			} else {
				http.Error(w, "Failed to delete runbook", http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

type RunbookApprovalResponse struct {
	ID          uuid.UUID `json:"id"`
	RunbookID   uuid.UUID `json:"runbook_id"`
	RunbookName string    `json:"runbook_name"`
	IncidentID  uuid.UUID `json:"incident_id"`
	Reason      string    `json:"reason"`
	Status      string    `json:"status"`
	RequestedBy string    `json:"requested_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// HandleGetRunbookApprovals lists SOAR auto-trigger approval requests for the tenant
// (defaults to status=pending; pass ?status=all for the full history).
func HandleGetRunbookApprovals(pgPool *pgxpool.Pool) http.HandlerFunc {
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

		list := make([]RunbookApprovalResponse, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT a.id, a.runbook_id, r.name, a.incident_id, a.reason, a.status, a.requested_by, a.created_at
				FROM runbook_approval_requests a
				JOIN tenant_runbooks r ON a.runbook_id = r.id
				WHERE a.tenant_id = $1 AND ($2 = 'all' OR a.status = $2)
				ORDER BY a.created_at DESC
				LIMIT 100
			`, tenantID, statusFilter)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var item RunbookApprovalResponse
				if err := rows.Scan(&item.ID, &item.RunbookID, &item.RunbookName, &item.IncidentID, &item.Reason, &item.Status, &item.RequestedBy, &item.CreatedAt); err != nil {
					return err
				}
				list = append(list, item)
			}
			return rows.Err()
		})

		if err != nil {
			http.Error(w, "Failed to query runbook approvals", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

type RunbookApprovalDecisionRequest struct {
	ApprovalID uuid.UUID `json:"approval_id"`
}

// HandleApproveRunbookRequest approves a pending SOAR auto-trigger request and executes it
// immediately using the same SSH/vault/TOFU path as manual runbook execution.
func HandleApproveRunbookRequest(pgPool *pgxpool.Pool) http.HandlerFunc {
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

		var req RunbookApprovalDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		var runbookID, incidentID uuid.UUID
		var name, script, vaultKeyPrefix string
		var execStatus, output string

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var status string
			if err := tx.QueryRow(ctx, `
				SELECT runbook_id, incident_id, status FROM runbook_approval_requests
				WHERE id = $1 AND tenant_id = $2
				FOR UPDATE
			`, req.ApprovalID, tenantID).Scan(&runbookID, &incidentID, &status); err != nil {
				return err
			}
			if status != "pending" {
				return fmt.Errorf("approval request is no longer pending (status: %s)", status)
			}

			if err := tx.QueryRow(ctx, "SELECT name, script, vault_key_host FROM tenant_runbooks WHERE id = $1", runbookID).Scan(&name, &script, &vaultKeyPrefix); err != nil {
				return err
			}

			masterKey, err := security.GetMasterKey()
			if err != nil {
				return fmt.Errorf("failed to retrieve encryption key: %w", err)
			}
			getSecret := func(key string) string {
				sec, err := vaultRepo.GetSecretByKey(ctx, tx, key)
				if err != nil {
					return ""
				}
				decrypted, err := security.Decrypt(sec.EncryptedValue, sec.Nonce, masterKey)
				if err != nil {
					return ""
				}
				return string(decrypted)
			}
			sshHost := getSecret(vaultKeyPrefix + "_host")
			sshUser := getSecret(vaultKeyPrefix + "_user")
			sshPass := getSecret(vaultKeyPrefix + "_password")
			sshPriv := getSecret(vaultKeyPrefix + "_private_key")
			if sshHost == "" || sshUser == "" {
				return fmt.Errorf("missing host or user secret in Vault under prefix: %s", vaultKeyPrefix)
			}

			log.Printf("[Runbook Approval] Executing approved runbook '%s' on %s@%s", name, sshUser, sshHost)
			output, err = ExecuteSSH(ctx, pgPool, vaultRepo, tenantID, vaultKeyPrefix, sshHost, sshUser, sshPass, sshPriv, script)
			execStatus = "sucesso"
			if err != nil {
				execStatus = "falha"
				output = fmt.Sprintf("Execution Error: %v\nLogs:\n%s", err, output)
			}

			if _, err := tx.Exec(ctx, `
				UPDATE runbook_approval_requests SET status = 'approved', approved_by = $1, approved_at = NOW() WHERE id = $2
			`, claims.UserID, req.ApprovalID); err != nil {
				return err
			}

			commentText := fmt.Sprintf("✅ **Runbook aprovado e executado [%s]**: Status: %s\n\n```bash\n%s\n```", name, execStatus, output)
			if _, err := tx.Exec(ctx, `
				INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
				VALUES ($1, $2, 'SRE Auto-Healing Co-Pilot', $3)
			`, incidentID, tenantID, commentText); err != nil {
				return err
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO runbook_execution_logs (tenant_id, runbook_id, incident_id, operator_name, script, output, status)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, tenantID, runbookID, incidentID, "SRE Operador (aprovação manual)", script, output, execStatus)
			return err
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to approve/execute runbook: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": execStatus, "output": output})
	}
}

// HandleRejectRunbookRequest rejects a pending SOAR auto-trigger approval request without executing it.
func HandleRejectRunbookRequest(pgPool *pgxpool.Pool) http.HandlerFunc {
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

		var req RunbookApprovalDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			var incidentID uuid.UUID
			var runbookName string
			if err := tx.QueryRow(ctx, `
				SELECT a.incident_id, r.name FROM runbook_approval_requests a
				JOIN tenant_runbooks r ON a.runbook_id = r.id
				WHERE a.id = $1 AND a.tenant_id = $2 AND a.status = 'pending'
			`, req.ApprovalID, tenantID).Scan(&incidentID, &runbookName); err != nil {
				return err
			}

			res, err := tx.Exec(ctx, `
				UPDATE runbook_approval_requests SET status = 'rejected', approved_by = $1, approved_at = NOW() WHERE id = $2
			`, claims.UserID, req.ApprovalID)
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return fmt.Errorf("approval request not found or no longer pending")
			}

			commentText := fmt.Sprintf("🚫 **Runbook rejeitado [%s]**: execução automática cancelada por um operador.", runbookName)
			_, err = tx.Exec(ctx, `
				INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
				VALUES ($1, $2, 'Sistema', $3)
			`, incidentID, tenantID, commentText)
			return err
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to reject request: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","message":"Runbook rejeitado com sucesso"}`))
	}
}

