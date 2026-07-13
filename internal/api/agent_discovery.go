package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Active network discovery (topology slice A). A tenant admin registers CIDR ranges (with an SNMP
// community) to sweep; the agent pulls these — community decrypted — via /agent/config, probes every
// host with an SNMP GET of sysName/sysDescr/sysObjectID, and pushes back the responders as discovered
// devices. This finds gear that is on the network but has never sent telemetry.

// maxDiscoveryAPIHosts mirrors the agent's cap: a target CIDR may not expand past a /20 (4096 hosts).
const maxDiscoveryAPIHosts = 4096

// AgentDiscoveryTarget is a range as handed to the agent (community decrypted). Config-only.
type AgentDiscoveryTarget struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	Community string `json:"community"`
}

// DiscoveryTargetSummary is the console view (no community secret).
type DiscoveryTargetSummary struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	CIDR    string    `json:"cidr"`
	Port    int       `json:"port"`
	Version string    `json:"version"`
}

// DiscoveryTargetInput is the create payload.
type DiscoveryTargetInput struct {
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	Community string `json:"community"`
}

// cidrHostCount returns how many addresses an IPv4 CIDR covers (pure, unit-tested). 0 with an error
// for anything unparseable or non-IPv4.
func cidrHostCount(cidr string) (int, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, fmt.Errorf("invalid CIDR")
	}
	if ipnet.IP.To4() == nil {
		return 0, fmt.Errorf("only IPv4 CIDRs are supported")
	}
	ones, bits := ipnet.Mask.Size()
	if bits-ones > 20 {
		return 0, fmt.Errorf("CIDR too large (max /12 span)")
	}
	return int(uint32(1) << uint(bits-ones)), nil
}

// validateDiscoveryTargetInput is pure and unit-tested.
func validateDiscoveryTargetInput(in DiscoveryTargetInput) error {
	if in.Name == "" || in.CIDR == "" {
		return fmt.Errorf("name and cidr are required")
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
	n, err := cidrHostCount(in.CIDR)
	if err != nil {
		return err
	}
	if n > maxDiscoveryAPIHosts {
		return fmt.Errorf("CIDR expands to %d hosts (max %d)", n, maxDiscoveryAPIHosts)
	}
	return nil
}

// loadAgentDiscoveryTargets returns the tenant's discovery targets with the community decrypted, for
// the agent. Must run inside the tenant RLS tx.
func loadAgentDiscoveryTargets(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, masterKey []byte) ([]AgentDiscoveryTarget, error) {
	rows, err := tx.Query(ctx, `SELECT id, name, cidr, port, version, community_encrypted, community_nonce FROM agent_discovery_targets WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentDiscoveryTarget, 0)
	for rows.Next() {
		var id uuid.UUID
		var t AgentDiscoveryTarget
		var enc, nonce []byte
		if err := rows.Scan(&id, &t.Name, &t.CIDR, &t.Port, &t.Version, &enc, &nonce); err != nil {
			return nil, err
		}
		plain, derr := security.DecryptForTenant(enc, nonce, masterKey, tenantID)
		if derr != nil {
			continue // skip a target whose community can't be decrypted rather than failing the whole config
		}
		t.ID = id.String()
		t.Community = string(plain)
		out = append(out, t)
	}
	return out, rows.Err()
}

// HandleGetDiscoveryTargets lists the tenant's discovery targets (no community). Any authenticated user.
func HandleGetDiscoveryTargets(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]DiscoveryTargetSummary, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `SELECT id, name, cidr, port, version FROM agent_discovery_targets WHERE tenant_id = $1 ORDER BY name`, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var s DiscoveryTargetSummary
				if e := rows.Scan(&s.ID, &s.Name, &s.CIDR, &s.Port, &s.Version); e != nil {
					return e
				}
				list = append(list, s)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to list discovery targets", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}

// HandleMutateDiscoveryTargets creates (POST) or deletes (DELETE ?id=) a discovery target. Route-gated
// to tenant admins.
func HandleMutateDiscoveryTargets(pgPool *pgxpool.Pool) http.HandlerFunc {
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
				_, e := tx.Exec(ctx, `DELETE FROM agent_discovery_targets WHERE id = $1 AND tenant_id = $2`, id, tenantID)
				return e
			})
			if err != nil {
				http.Error(w, "Failed to delete discovery target", http.StatusInternalServerError)
				return
			}
			auditSNMP(ctx, pgPool, tenantID, claims, "agent.discovery_target.delete", id.String(), r.RemoteAddr, nil)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var in DiscoveryTargetInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}
		if verr := validateDiscoveryTargetInput(in); verr != nil {
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

		var newID uuid.UUID
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO agent_discovery_targets (tenant_id, name, cidr, port, version, community_encrypted, community_nonce)
				VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id
			`, tenantID, in.Name, in.CIDR, in.Port, in.Version, enc, nonce).Scan(&newID)
		})
		if err != nil {
			http.Error(w, "Failed to create discovery target", http.StatusInternalServerError)
			return
		}
		auditSNMP(ctx, pgPool, tenantID, claims, "agent.discovery_target.create", newID.String(), r.RemoteAddr,
			map[string]interface{}{"cidr": in.CIDR})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": newID})
	}
}

// DiscoveredDeviceIn is one device the agent reports from a sweep.
type DiscoveredDeviceIn struct {
	IP          string `json:"ip"`
	SysName     string `json:"sysname"`
	SysDescr    string `json:"sysdescr"`
	SysObjectID string `json:"sysobjectid"`
	Vendor      string `json:"vendor"`
	DeviceType  string `json:"device_type"`
}

// AgentDiscoveryRequest is the agent's discovery push batch.
type AgentDiscoveryRequest struct {
	AgentID uuid.UUID            `json:"agent_id"`
	Devices []DiscoveredDeviceIn `json:"devices"`
}

// HandleAgentDiscovery upserts the batch of discovered devices into the tenant inventory. API-key auth
// (rides the ingest guard). Re-observing a device refreshes last_seen/identity instead of duplicating.
func HandleAgentDiscovery(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var req AgentDiscoveryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: invalid payload", http.StatusBadRequest)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		accepted := 0
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			for _, d := range req.Devices {
				if net.ParseIP(d.IP) == nil {
					continue // ignore a malformed IP rather than failing the batch
				}
				_, e := tx.Exec(ctx, `
					INSERT INTO discovered_devices (tenant_id, ip, sysname, sysdescr, sysobjectid, vendor, device_type, first_seen, last_seen)
					VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
					ON CONFLICT (tenant_id, ip) DO UPDATE SET
						sysname = EXCLUDED.sysname,
						sysdescr = EXCLUDED.sysdescr,
						sysobjectid = EXCLUDED.sysobjectid,
						vendor = EXCLUDED.vendor,
						device_type = EXCLUDED.device_type,
						last_seen = NOW()
				`, tenantID, d.IP, d.SysName, d.SysDescr, d.SysObjectID, d.Vendor, d.DeviceType)
				if e != nil {
					return e
				}
				accepted++
			}
			return nil
		})
		if err != nil {
			http.Error(w, "Failed to store discovered devices", http.StatusInternalServerError)
			return
		}

		// Liveness: touch the agent's last_seen (same as events/metrics).
		touchAgent(r, pgPool, tenantID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"accepted": accepted})
	}
}

// DiscoveredDevice is the console view of one inventoried device.
type DiscoveredDevice struct {
	ID          uuid.UUID `json:"id"`
	IP          string    `json:"ip"`
	SysName     string    `json:"sysname"`
	SysDescr    string    `json:"sysdescr"`
	SysObjectID string    `json:"sysobjectid"`
	Vendor      string    `json:"vendor"`
	DeviceType  string    `json:"device_type"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// HandleGetDiscoveredDevices returns the tenant's discovered-device inventory (JWT, tenant-scoped).
func HandleGetDiscoveredDevices(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		list := make([]DiscoveredDevice, 0)
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, e := tx.Query(ctx, `
				SELECT id, ip, sysname, sysdescr, sysobjectid, vendor, device_type, first_seen, last_seen
				FROM discovered_devices WHERE tenant_id = $1 ORDER BY last_seen DESC LIMIT 500
			`, tenantID)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var d DiscoveredDevice
				if e := rows.Scan(&d.ID, &d.IP, &d.SysName, &d.SysDescr, &d.SysObjectID, &d.Vendor, &d.DeviceType, &d.FirstSeen, &d.LastSeen); e != nil {
					return e
				}
				list = append(list, d)
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, "Failed to list discovered devices", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	}
}
