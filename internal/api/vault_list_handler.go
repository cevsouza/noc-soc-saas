package api

import (
	"encoding/json"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VaultSecretMetadata struct {
	ID          uuid.UUID `json:"id"`
	SecretKey   string    `json:"secret_key"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// HandleGetVaultSecrets lists all vault secret metadata for a tenant (without actual secret values)
func HandleGetVaultSecrets(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]VaultSecretMetadata, 0)

		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			query := `
				SELECT id, secret_key, description, created_at, updated_at 
				FROM tenant_vault 
				WHERE tenant_id = $1 
				ORDER BY secret_key
			`
			rows, err := tx.Query(ctx, query, tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()

			for rows.Next() {
				var meta VaultSecretMetadata
				err := rows.Scan(&meta.ID, &meta.SecretKey, &meta.Description, &meta.CreatedAt, &meta.UpdatedAt)
				if err != nil {
					return err
				}
				list = append(list, meta)
			}
			return rows.Err()
		})

		if err != nil {
			http.Error(w, "Failed to query vault secrets", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}
