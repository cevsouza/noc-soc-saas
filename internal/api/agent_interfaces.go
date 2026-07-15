package api

import (
	"encoding/json"
	"net/http"

	"noc-api/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Per-interface utilization ingest (topology slice T-D). The agent walks each SNMP-monitored device's
// ifTable/ifXTable and pushes the latest in/out bps, speed and oper status per interface; the topology
// graph joins these to the LLDP/CDP edges (device_ip + ifindex = the link's local_port) to color the
// links by real load. Latest sample only (upsert).

// InterfaceStatIn is one interface's utilization snapshot from the agent.
type InterfaceStatIn struct {
	DeviceIP   string `json:"device_ip"`
	IfIndex    string `json:"ifindex"`
	IfName     string `json:"ifname"`
	OperStatus string `json:"oper_status"`
	InBps      int64  `json:"in_bps"`
	OutBps     int64  `json:"out_bps"`
	SpeedBps   int64  `json:"speed_bps"`
}

// AgentInterfacesRequest is the agent's interface push batch.
type AgentInterfacesRequest struct {
	Interfaces []InterfaceStatIn `json:"interfaces"`
}

// HandleAgentInterfaces upserts a batch of interface stats (API-key auth) and refreshes agent liveness.
func HandleAgentInterfaces(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := db.TenantIDFromContext(r.Context())
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		var req AgentInterfacesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request: invalid payload", http.StatusBadRequest)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		accepted := 0
		err := db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			for _, s := range req.Interfaces {
				if s.DeviceIP == "" || s.IfIndex == "" {
					continue
				}
				if _, e := tx.Exec(ctx, `
					INSERT INTO agent_interfaces (tenant_id, device_ip, ifindex, ifname, oper_status, in_bps, out_bps, speed_bps, updated_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
					ON CONFLICT (tenant_id, device_ip, ifindex) DO UPDATE SET
						ifname = EXCLUDED.ifname,
						oper_status = EXCLUDED.oper_status,
						in_bps = EXCLUDED.in_bps,
						out_bps = EXCLUDED.out_bps,
						speed_bps = EXCLUDED.speed_bps,
						updated_at = NOW()
				`, tenantID, s.DeviceIP, s.IfIndex, s.IfName, s.OperStatus, s.InBps, s.OutBps, s.SpeedBps); e != nil {
					return e
				}
				accepted++
			}
			return nil
		})
		if err != nil {
			http.Error(w, "Failed to store interfaces", http.StatusInternalServerError)
			return
		}

		touchAgent(r, pgPool, tenantID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"accepted": accepted})
	}
}
