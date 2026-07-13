package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SNMPCheck is one threshold rule on a polled OID. On a breach (the polled numeric value satisfies
// comparison vs threshold) the agent emits an alert.
type SNMPCheck struct {
	OID        string  `json:"oid"`
	Label      string  `json:"label"`
	Comparison string  `json:"comparison"` // gt, lt, ge, le, eq, ne
	Threshold  float64 `json:"threshold"`
	Severity   string  `json:"severity"` // info, warning, critical, fatal
}

// AgentSNMPTarget is a target as handed to the agent (community decrypted). Config-only.
type AgentSNMPTarget struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	Version   string      `json:"version"`
	Community string      `json:"community"`
	Checks    []SNMPCheck `json:"checks"`
}

// SNMPTargetSummary is the console view (no community secret).
type SNMPTargetSummary struct {
	ID      uuid.UUID   `json:"id"`
	Name    string      `json:"name"`
	Host    string      `json:"host"`
	Port    int         `json:"port"`
	Version string      `json:"version"`
	Checks  []SNMPCheck `json:"checks"`
}

var validSNMPComparisons = map[string]bool{"gt": true, "lt": true, "ge": true, "le": true, "eq": true, "ne": true}
var validSNMPSeverities = map[string]bool{"info": true, "warning": true, "critical": true, "fatal": true}

// SNMPTargetInput is the create payload.
type SNMPTargetInput struct {
	Name      string      `json:"name"`
	Host      string      `json:"host"`
	Port      int         `json:"port"`
	Version   string      `json:"version"`
	Community string      `json:"community"`
	Checks    []SNMPCheck `json:"checks"`
}

// validateSNMPTargetInput is pure and unit-tested.
func validateSNMPTargetInput(in SNMPTargetInput) error {
	if in.Name == "" || in.Host == "" {
		return fmt.Errorf("name and host are required")
	}
	if in.Community == "" {
		return fmt.Errorf("community is required")
	}
	if in.Port != 0 && (in.Port < 1 || in.Port > 65535) {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if in.Version != "" && in.Version != "2c" {
		return fmt.Errorf("only SNMP version 2c is supported")
	}
	if len(in.Checks) == 0 {
		return fmt.Errorf("at least one check is required")
	}
	for i, c := range in.Checks {
		if c.OID == "" || c.Label == "" {
			return fmt.Errorf("check %d: oid and label are required", i)
		}
		if !validSNMPComparisons[c.Comparison] {
			return fmt.Errorf("check %d: invalid comparison %q", i, c.Comparison)
		}
		if !validSNMPSeverities[c.Severity] {
			return fmt.Errorf("check %d: invalid severity %q", i, c.Severity)
		}
	}
	return nil
}

// loadAgentSNMPTargets returns the tenant's SNMP targets with the community decrypted, for the agent.
// Must run inside the tenant RLS tx.
func loadAgentSNMPTargets(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, masterKey []byte) ([]AgentSNMPTarget, error) {
	rows, err := tx.Query(ctx, `SELECT id, name, host, port, version, community_encrypted, community_nonce, checks FROM agent_snmp_targets WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentSNMPTarget, 0)
	for rows.Next() {
		var id uuid.UUID
		var t AgentSNMPTarget
		var enc, nonce, checksJSON []byte
		if err := rows.Scan(&id, &t.Name, &t.Host, &t.Port, &t.Version, &enc, &nonce, &checksJSON); err != nil {
			return nil, err
		}
		plain, derr := security.DecryptForTenant(enc, nonce, masterKey, tenantID)
		if derr != nil {
			continue // skip a target whose community can't be decrypted rather than failing the whole config
		}
		t.ID = id.String()
		t.Community = string(plain)
		_ = json.Unmarshal(checksJSON, &t.Checks)
		out = append(out, t)
	}
	return out, rows.Err()
}

// HandleGetSNMPTargets lists the tenant's SNMP targets (no community). Any authenticated user.
func HandleGetSNMPTargets(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]SNMPTargetSummary, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `SELECT id, name, host, port, version, checks FROM agent_snmp_targets WHERE tenant_id = $1 ORDER BY name`, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var s SNMPTargetSummary
				var checksJSON []byte
				if e := rows.Scan(&s.ID, &s.Name, &s.Host, &s.Port, &s.Version, &checksJSON); e != nil {
					return e
				}
				_ = json.Unmarshal(checksJSON, &s.Checks)
				list = append(list, s)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to list SNMP targets", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleMutateSNMPTargets creates (POST) or deletes (DELETE ?id=) an SNMP target. Route-gated to
// tenant admins.
func HandleMutateSNMPTargets(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)

		if r.Method == http.MethodDelete {
			id, perr := uuid.Parse(r.URL.Query().Get("id"))
			if perr != nil {
				http.Error(w, "Bad Request: valid id is required", http.StatusBadRequest)
				return
			}
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `DELETE FROM agent_snmp_targets WHERE id = $1 AND tenant_id = $2`, id, tenantID)
				return e
			})
			if err != nil {
				http.Error(w, "Failed to delete SNMP target", http.StatusInternalServerError)
				return
			}
			auditSNMP(ctx, pgPool, tenantID, claims, "agent.snmp_target.delete", id.String(), r.RemoteAddr, nil)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var in SNMPTargetInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if verr := validateSNMPTargetInput(in); verr != nil {
			http.Error(w, "Bad Request: "+verr.Error(), http.StatusBadRequest)
			return
		}
		if in.Port == 0 {
			in.Port = 161
		}
		if in.Version == "" {
			in.Version = "2c"
		}

		masterKey, err := security.GetMasterKey()
		if err != nil {
			http.Error(w, "Server key unavailable", http.StatusInternalServerError)
			return
		}
		enc, nonce, err := security.EncryptForTenant([]byte(in.Community), masterKey, tenantID)
		if err != nil {
			http.Error(w, "Failed to encrypt community", http.StatusInternalServerError)
			return
		}
		checksJSON, _ := json.Marshal(in.Checks)

		var newID uuid.UUID
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO agent_snmp_targets (tenant_id, name, host, port, version, community_encrypted, community_nonce, checks)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id
			`, tenantID, in.Name, in.Host, in.Port, in.Version, enc, nonce, checksJSON).Scan(&newID)
		})
		if err != nil {
			http.Error(w, "Failed to create SNMP target", http.StatusInternalServerError)
			return
		}
		auditSNMP(ctx, pgPool, tenantID, claims, "agent.snmp_target.create", newID.String(), r.RemoteAddr,
			map[string]interface{}{"host": in.Host, "checks": len(in.Checks)})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": newID})
	}
}

func auditSNMP(ctx context.Context, pgPool *pgxpool.Pool, tenantID uuid.UUID, claims *middleware.JWTClaims, action, resource, ip string, details map[string]interface{}) {
	var actorID uuid.UUID
	if claims != nil {
		actorID = claims.UserID
	}
	audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: action, Resource: resource, Details: details, IPAddress: ip})
}
