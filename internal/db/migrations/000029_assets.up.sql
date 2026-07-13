-- 000029_assets.up.sql
--
-- CMDB de verdade (topology slice T2). Until now the "Topologia CMDB & Ativos" tab only had an
-- auto-discovered inventory (discovered_devices) with zero manual attributes — it wasn't a real CMDB:
-- you couldn't record business criticality, owner, location, tags or notes, nor register an asset that
-- has no SNMP (a cloud service, a non-SNMP appliance). This table is the managed overlay/registry:
--
--   * identifier is the stable key that links an asset to discovery and to the alert stream — the IP
--     or hostname. For a pure-manual asset (no discovery) it's a user-provided label.
--   * The row is an OVERLAY: for a discovered device the CMDB fields annotate it; a device with no
--     asset row simply reads as unmanaged (criticality defaults to 'medium' in the view). The merge
--     happens at read time (GET /api/v1/assets), so the agent's discovery upsert never touches this.
--
-- Payoff: business_criticality is exactly the "asset criticality" input the dynamic risk score (R3)
-- needs — the worker can look it up by the incident's host/ip once wired.
--
-- FORCE RLS + tenant-isolation policy, consistent with every tenant table.

CREATE TABLE IF NOT EXISTS assets (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    identifier           VARCHAR(128) NOT NULL,
    name                 VARCHAR(255) NOT NULL,
    asset_type           VARCHAR(64) NOT NULL DEFAULT '',
    vendor               VARCHAR(64) NOT NULL DEFAULT '',
    business_criticality VARCHAR(16) NOT NULL DEFAULT 'medium'
        CHECK (business_criticality IN ('low', 'medium', 'high', 'critical')),
    owner                VARCHAR(255) NOT NULL DEFAULT '',
    location             VARCHAR(255) NOT NULL DEFAULT '',
    tags                 TEXT[] NOT NULL DEFAULT '{}',
    notes                TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, identifier)
);
CREATE INDEX IF NOT EXISTS idx_assets_tenant ON assets (tenant_id);

ALTER TABLE assets ENABLE ROW LEVEL SECURITY;
ALTER TABLE assets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON assets
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON assets TO noc_app_runtime;
    END IF;
END
$$;
