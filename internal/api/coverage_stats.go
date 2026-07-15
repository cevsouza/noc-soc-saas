package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Monitoring-coverage KPI (K2): of the devices the agent discovered on the network (agent_discovery
// SNMP sweep → discovered_devices), how many are actually being monitored? A discovered device with
// no monitoring signal is "dark" — on the network but nothing watching it, the actionable gap.
//
// A device counts as covered if its ip or sysName matches a monitored host, where "monitored hosts"
// is the union of (a) hosts that have produced telemetry/alerts (alerts.ai_analysis->>'host') and
// (b) hosts configured as SNMP poll targets (agent_snmp_targets.host). Matching is by exact ip or
// sysName — a deliberately conservative join; a device is only "covered" when there's real evidence.

const maxSilentDevicesReturned = 100

// SilentDevice is a discovered device with no monitoring signal — surfaced so an operator can go set
// up monitoring for it.
type SilentDevice struct {
	IP         string    `json:"ip"`
	SysName    string    `json:"sysname"`
	Vendor     string    `json:"vendor"`
	DeviceType string    `json:"device_type"`
	LastSeen   time.Time `json:"last_seen"`
}

// CoverageStats is the monitoring-coverage posture for a tenant.
type CoverageStats struct {
	TotalDiscovered int            `json:"total_discovered"`
	Covered         int            `json:"covered"`
	CoveragePct     float64        `json:"coverage_pct"`
	SilentDevices   []SilentDevice `json:"silent_devices"`
}

// coveragePct returns covered/total as a percentage, guarding division by zero (0 discovered → 0%).
func coveragePct(covered, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(covered) / float64(total) * 100
}

// HandleGetCoverageStats reports monitoring coverage for the caller's tenant. Read-only, same access
// level as the other KPI endpoints; all reads run inside the tenant-scoped RLS transaction.
func HandleGetCoverageStats(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		stats := CoverageStats{SilentDevices: []SilentDevice{}}
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				WITH monitored AS (
					SELECT DISTINCT ai_analysis->>'host' AS h
					FROM alerts
					WHERE tenant_id = $1 AND ai_analysis->>'host' IS NOT NULL AND ai_analysis->>'host' <> ''
					UNION
					SELECT DISTINCT host FROM agent_snmp_targets WHERE tenant_id = $1
				)
				SELECT dd.ip, dd.sysname, dd.vendor, dd.device_type, dd.last_seen,
					(dd.ip IN (SELECT h FROM monitored)
						OR (dd.sysname <> '' AND dd.sysname IN (SELECT h FROM monitored))) AS covered
				FROM discovered_devices dd
				WHERE dd.tenant_id = $1
				ORDER BY dd.last_seen DESC
			`, tenantID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var d SilentDevice
				var covered bool
				if err := rows.Scan(&d.IP, &d.SysName, &d.Vendor, &d.DeviceType, &d.LastSeen, &covered); err != nil {
					return err
				}
				stats.TotalDiscovered++
				if covered {
					stats.Covered++
				} else if len(stats.SilentDevices) < maxSilentDevicesReturned {
					stats.SilentDevices = append(stats.SilentDevices, d)
				}
			}
			return rows.Err()
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to compute coverage stats: %v", err), http.StatusInternalServerError)
			return
		}
		stats.CoveragePct = coveragePct(stats.Covered, stats.TotalDiscovered)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}
