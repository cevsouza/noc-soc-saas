-- 000030_asset_aliases.up.sql
--
-- Robust host↔device matching (topology slice T3). The topology graph folds an alert-host onto a
-- discovered device only when the host string matches the device IP or sysName. Monitoring tools rarely
-- emit exactly the sysName (they use FQDNs, short names, DNS aliases), so the same box shows up twice —
-- once as a discovered device and once as a telemetry-only host. Automatic normalization (FQDN vs short
-- name, case) covers the common cases; `aliases` lets an operator manually map the remaining hostnames a
-- tool uses for an asset onto that asset. It's a CMDB attribute (T2), edited in the CMDB panel.

ALTER TABLE assets ADD COLUMN IF NOT EXISTS aliases TEXT[] NOT NULL DEFAULT '{}';
