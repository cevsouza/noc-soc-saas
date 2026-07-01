package api

import (
	"bytes"
	"context"
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
}

type ExecuteRunbookRequest struct {
	RunbookID  uuid.UUID `json:"runbook_id"`
	IncidentID uuid.UUID `json:"incident_id"`
}

// Helper function to resolve tenant ID (supports query override for global operators)
func resolveTenantID(r *http.Request) (uuid.UUID, bool) {
	if tenantParam := r.URL.Query().Get("tenant_id"); tenantParam != "" {
		if id, err := uuid.Parse(tenantParam); err == nil {
			return id, true
		}
	}
	return db.TenantIDFromContext(r.Context())
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
			CreatedAt    time.Time `json:"created_at"`
		}

		list := make([]RunbookResponse, 0)
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, "SELECT id, tenant_id, name, trigger_rule, script, vault_key_host, created_at FROM tenant_runbooks WHERE tenant_id = $1 ORDER BY name", tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var rb RunbookResponse
				err := rows.Scan(&rb.ID, &rb.TenantID, &rb.Name, &rb.TriggerRule, &rb.Script, &rb.VaultKeyHost, &rb.CreatedAt)
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
			query := "INSERT INTO tenant_runbooks (tenant_id, name, trigger_rule, script, vault_key_host) VALUES ($1, $2, $3, $4, $5) RETURNING id"
			return tx.QueryRow(ctx, query, tenantID, req.Name, req.TriggerRule, req.Script, req.VaultKeyHost).Scan(&runbookID)
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
			err := tx.QueryRow(ctx, "SELECT name, script, vault_key_host FROM tenant_runbooks WHERE id = $1 AND tenant_id = $2", req.RunbookID, tenantID).Scan(&name, &script, &vaultKeyPrefix)
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
