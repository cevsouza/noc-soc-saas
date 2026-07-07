package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"noc-api/internal/db"
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
}

type ExecuteRunbookRequest struct {
	RunbookID  uuid.UUID `json:"runbook_id"`
	IncidentID uuid.UUID `json:"incident_id"`
}

// HandleGetRunbooks returns the list of runbooks for the current tenant.
func HandleGetRunbooks(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		type RunbookResponse struct {
			ID           uuid.UUID `json:"id"`
			TenantID     uuid.UUID `json:"tenant_id"`
			Name         string    `json:"name"`
			TriggerRule  string    `json:"trigger_rule"`
			Script       string    `json:"script"`
			VaultKeyHost string    `json:"vault_key_host"`
			IsGlobal     bool      `json:"is_global"`
			CreatedAt    time.Time `json:"created_at"`
		}

		list := make([]RunbookResponse, 0)
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT id, tenant_id, name, trigger_rule, script, vault_key_host, is_global, created_at FROM tenant_runbooks WHERE tenant_id = $1 OR is_global = TRUE ORDER BY name", tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var rb RunbookResponse
				err := rows.Scan(&rb.ID, &rb.TenantID, &rb.Name, &rb.TriggerRule, &rb.Script, &rb.VaultKeyHost, &rb.IsGlobal, &rb.CreatedAt)
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

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleCreateRunbook registers a new runbook for the tenant.
func HandleCreateRunbook(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
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
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := "INSERT INTO tenant_runbooks (tenant_id, name, trigger_rule, script, vault_key_host, is_global) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id"
			return tx.QueryRow(ctx, query, tenantID, req.Name, req.TriggerRule, req.Script, req.VaultKeyHost, req.IsGlobal).Scan(&runbookID)
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
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
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

		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
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
			output, err = ExecuteSSH(sshHost, sshUser, sshPass, sshPriv, script)
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

// ExecuteSSH executes script commands on a remote host.
func ExecuteSSH(host, username, password, privateKey, command string) (string, error) {
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

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]RunbookAuditResponse, 0)
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
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
		tenantID, ok := resolveTenantID(r)
		if !ok {
			http.Error(w, "Unauthorized: Tenant context not found", http.StatusUnauthorized)
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

