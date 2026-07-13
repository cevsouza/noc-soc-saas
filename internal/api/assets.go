package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"noc-api/internal/audit"
	"noc-api/internal/db"
	"noc-api/internal/middleware"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CMDB (topology slice T2). `assets` is the managed overlay/registry on top of the auto-discovered
// inventory: it lets an operator record business criticality, owner, location, tags and notes for an
// asset — and register assets that have no SNMP at all (a cloud service, a non-SNMP appliance). The
// merge with discovered_devices happens at read time so the agent's discovery upsert never clobbers
// the manual fields.

// Asset mirrors an assets row (the manual overlay).
type Asset struct {
	Identifier          string   `json:"identifier"`
	Name                string   `json:"name"`
	AssetType           string   `json:"asset_type"`
	Vendor              string   `json:"vendor"`
	BusinessCriticality string   `json:"business_criticality"`
	Owner               string   `json:"owner"`
	Location            string   `json:"location"`
	Tags                []string `json:"tags"`
	Notes               string   `json:"notes"`
	// Aliases are alternate hostnames/IPs a monitoring tool uses for this asset, so the topology graph
	// can fold those alert-hosts onto the right node (topology slice T3).
	Aliases []string `json:"aliases"`
}

// AssetView is one row of the merged CMDB list: the managed overlay (if any) plus the discovery facts
// (if any). `managed` = there is an assets row; `discovered` = it responded to the SNMP sweep.
type AssetView struct {
	Identifier          string     `json:"identifier"`
	Name                string     `json:"name"`
	AssetType           string     `json:"asset_type"`
	Vendor              string     `json:"vendor"`
	BusinessCriticality string     `json:"business_criticality"`
	Owner               string     `json:"owner"`
	Location            string     `json:"location"`
	Tags                []string   `json:"tags"`
	Notes               string     `json:"notes"`
	Aliases             []string   `json:"aliases"`
	Managed             bool       `json:"managed"`
	Discovered          bool       `json:"discovered"`
	SysName             string     `json:"sysname,omitempty"`
	LastSeen            *time.Time `json:"last_seen,omitempty"`
}

// discoveredRow is the subset of a discovered_devices row the merge needs.
type discoveredRow struct {
	IP         string
	SysName    string
	Vendor     string
	DeviceType string
	LastSeen   time.Time
}

// validBusinessCriticality are the allowed criticality levels (mirrored by the DB CHECK constraint).
var validBusinessCriticality = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}

// criticalityRank orders the CMDB list most-critical first. Unmanaged assets read as "medium".
func criticalityRank(c string) int {
	switch c {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 2
	}
}

// normalizeTags trims, drops empties, and de-duplicates a tag list (pure, testable).
func normalizeTags(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}

// validateAssetInput validates a POST body without touching the DB (unit-testable).
func validateAssetInput(a Asset) error {
	if strings.TrimSpace(a.Identifier) == "" {
		return fmt.Errorf("identifier is required")
	}
	if len(a.Identifier) > 128 {
		return fmt.Errorf("identifier too long (max 128)")
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if a.BusinessCriticality != "" && !validBusinessCriticality[a.BusinessCriticality] {
		return fmt.Errorf("invalid business_criticality %q (expected low/medium/high/critical)", a.BusinessCriticality)
	}
	return nil
}

// mergeAssets folds the manual overlay (assets) and the discovered inventory (discovered_devices) into
// one CMDB list. Pure and unit-tested (no DB). A discovered device with a matching asset row becomes a
// managed+discovered entry; without one it reads as unmanaged (criticality "medium"); an asset whose
// identifier isn't a discovered IP is a manual-only entry.
func mergeAssets(assets []Asset, devices []discoveredRow) []AssetView {
	byID := map[string]Asset{}
	for _, a := range assets {
		byID[a.Identifier] = a
	}
	seen := map[string]bool{}
	out := []AssetView{}

	for _, d := range devices {
		v := AssetView{
			Identifier:          d.IP,
			Name:                d.SysName,
			AssetType:           d.DeviceType,
			Vendor:              d.Vendor,
			BusinessCriticality: "medium",
			Tags:                []string{},
			Aliases:             []string{},
			Discovered:          true,
			SysName:             d.SysName,
		}
		if v.Name == "" {
			v.Name = d.IP
		}
		ls := d.LastSeen
		v.LastSeen = &ls
		if a, ok := byID[d.IP]; ok {
			applyOverlay(&v, a)
			v.Managed = true
			seen[d.IP] = true
		}
		out = append(out, v)
	}

	// Manual-only assets (identifier not a discovered IP).
	for _, a := range assets {
		if seen[a.Identifier] {
			continue
		}
		v := AssetView{Identifier: a.Identifier, BusinessCriticality: "medium", Tags: []string{}, Aliases: []string{}, Managed: true}
		applyOverlay(&v, a)
		out = append(out, v)
	}

	sort.Slice(out, func(i, j int) bool {
		ri, rj := criticalityRank(out[i].BusinessCriticality), criticalityRank(out[j].BusinessCriticality)
		if ri != rj {
			return ri > rj
		}
		return out[i].Identifier < out[j].Identifier
	})
	return out
}

// applyOverlay copies the manual CMDB fields onto a view, keeping discovery values when a manual field
// is blank (so annotating only the criticality doesn't wipe the discovered name/vendor/type).
func applyOverlay(v *AssetView, a Asset) {
	if a.Name != "" {
		v.Name = a.Name
	}
	if a.AssetType != "" {
		v.AssetType = a.AssetType
	}
	if a.Vendor != "" {
		v.Vendor = a.Vendor
	}
	if a.BusinessCriticality != "" {
		v.BusinessCriticality = a.BusinessCriticality
	}
	v.Owner = a.Owner
	v.Location = a.Location
	if a.Tags != nil {
		v.Tags = a.Tags
	}
	if a.Aliases != nil {
		v.Aliases = a.Aliases
	}
	v.Notes = a.Notes
}

// HandleGetAssets returns the merged CMDB list for the tenant.
func HandleGetAssets(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		ctx := db.WithTenantID(r.Context(), tenantID)

		var assets []Asset
		var devices []discoveredRow
		err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
			ar, e := tx.Query(ctx, `
				SELECT identifier, name, asset_type, vendor, business_criticality, owner, location, tags, notes, aliases
				FROM assets WHERE tenant_id = $1`, tenantID)
			if e != nil {
				return e
			}
			for ar.Next() {
				var a Asset
				if e := ar.Scan(&a.Identifier, &a.Name, &a.AssetType, &a.Vendor, &a.BusinessCriticality, &a.Owner, &a.Location, &a.Tags, &a.Notes, &a.Aliases); e != nil {
					ar.Close()
					return e
				}
				assets = append(assets, a)
			}
			ar.Close()

			dr, e := tx.Query(ctx, `
				SELECT ip, sysname, vendor, device_type, last_seen
				FROM discovered_devices WHERE tenant_id = $1`, tenantID)
			if e != nil {
				return e
			}
			for dr.Next() {
				var d discoveredRow
				if e := dr.Scan(&d.IP, &d.SysName, &d.Vendor, &d.DeviceType, &d.LastSeen); e != nil {
					dr.Close()
					return e
				}
				devices = append(devices, d)
			}
			dr.Close()
			return nil
		})
		if err != nil {
			http.Error(w, "Failed to query assets", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mergeAssets(assets, devices))
	}
}

// HandleMutateAssets upserts (POST) or deletes (DELETE ?identifier=) an asset annotation. Gated to
// tenant admins at the route level; audited. Upsert by (tenant_id, identifier) so re-annotating a
// discovered device updates in place instead of duplicating.
func HandleMutateAssets(pgPool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, err := middleware.ResolveTenantScope(r.Context(), r, pgPool)
		if err != nil {
			middleware.WriteScopeError(w, err)
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		ctx := db.WithTenantID(r.Context(), tenantID)
		var actorID uuid.UUID
		if claims != nil {
			actorID = claims.UserID
		}

		switch r.Method {
		case http.MethodPost:
			var a Asset
			if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
				http.Error(w, "Invalid request payload", http.StatusBadRequest)
				return
			}
			a.Identifier = strings.TrimSpace(a.Identifier)
			a.Name = strings.TrimSpace(a.Name)
			if a.BusinessCriticality == "" {
				a.BusinessCriticality = "medium"
			}
			a.Tags = normalizeTags(a.Tags)
			a.Aliases = normalizeTags(a.Aliases)
			if verr := validateAssetInput(a); verr != nil {
				http.Error(w, "Bad Request: "+verr.Error(), http.StatusBadRequest)
				return
			}
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				_, e := tx.Exec(ctx, `
					INSERT INTO assets (tenant_id, identifier, name, asset_type, vendor, business_criticality, owner, location, tags, notes, aliases, updated_at)
					VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
					ON CONFLICT (tenant_id, identifier) DO UPDATE SET
						name = EXCLUDED.name,
						asset_type = EXCLUDED.asset_type,
						vendor = EXCLUDED.vendor,
						business_criticality = EXCLUDED.business_criticality,
						owner = EXCLUDED.owner,
						location = EXCLUDED.location,
						tags = EXCLUDED.tags,
						notes = EXCLUDED.notes,
						aliases = EXCLUDED.aliases,
						updated_at = NOW()
				`, tenantID, a.Identifier, a.Name, a.AssetType, a.Vendor, a.BusinessCriticality, a.Owner, a.Location, a.Tags, a.Notes, a.Aliases)
				return e
			})
			if err != nil {
				http.Error(w, "Failed to save asset", http.StatusInternalServerError)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "asset.upsert", Resource: a.Identifier, IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		case http.MethodDelete:
			identifier := strings.TrimSpace(r.URL.Query().Get("identifier"))
			if identifier == "" {
				http.Error(w, "Bad Request: identifier is required", http.StatusBadRequest)
				return
			}
			var affected int64
			err = db.ExecuteInTenantTx(ctx, pgPool, func(tx pgx.Tx) error {
				res, e := tx.Exec(ctx, `DELETE FROM assets WHERE tenant_id = $1 AND identifier = $2`, tenantID, identifier)
				if e != nil {
					return e
				}
				affected = res.RowsAffected()
				return nil
			})
			if err != nil {
				http.Error(w, "Failed to delete asset", http.StatusInternalServerError)
				return
			}
			if affected == 0 {
				http.Error(w, "Asset not found", http.StatusNotFound)
				return
			}
			audit.Record(ctx, pgPool, audit.Entry{TenantID: tenantID, UserID: actorID, Action: "asset.delete", Resource: identifier, IPAddress: r.RemoteAddr})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	}
}
