-- 000034_threat_intel.up.sql
--
-- Cross-tenant threat intel (Backlog B6 fatia 1). The network effect: when one tenant observes a
-- malicious indicator (a public IP for now), opted-in tenants collectively benefit from the sighting.
--
-- Privacy model:
--   * Opt-in only — a tenant contributes and reads shared intel only when threat_intel_opt_in is true.
--   * The shared tables are GLOBAL (no RLS) on purpose: they are a cross-tenant aggregate. They store
--     NO raw tenant_id. Distinct-contributor counting is done via a pseudonymous tenant_hash
--     (HMAC-SHA256 of the tenant id under the server's VAULT_MASTER_KEY), so a leak of these tables
--     never reveals which tenant saw which indicator, and the read API exposes only aggregate counts.
--   * The worker records only PUBLIC IPs (RFC1918 / loopback / link-local are dropped), so internal
--     topology is never shared.

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS threat_intel_opt_in BOOLEAN NOT NULL DEFAULT false;

-- Aggregate per indicator (global, no RLS).
CREATE TABLE IF NOT EXISTS threat_intel_indicators (
    indicator_type    VARCHAR(20) NOT NULL,       -- 'ip' (domain/hash added in B6 fatia 2)
    indicator_value   VARCHAR(255) NOT NULL,
    observation_count BIGINT NOT NULL DEFAULT 0,   -- total sightings across the fleet
    tenant_count      INTEGER NOT NULL DEFAULT 0,  -- distinct opted-in tenants that saw it
    first_seen        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (indicator_type, indicator_value)
);

CREATE INDEX IF NOT EXISTS idx_threat_intel_last_seen ON threat_intel_indicators (last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_threat_intel_tenant_count ON threat_intel_indicators (tenant_count DESC);

-- Pseudonymous distinct-contributor ledger (global, no RLS). PK dedupes repeat sightings by the same
-- tenant so tenant_count reflects distinct contributors, without ever storing a raw tenant id.
CREATE TABLE IF NOT EXISTS threat_intel_contributions (
    indicator_type  VARCHAR(20) NOT NULL,
    indicator_value VARCHAR(255) NOT NULL,
    tenant_hash     CHAR(64) NOT NULL,  -- HMAC-SHA256 hex of the tenant id
    first_seen      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (indicator_type, indicator_value, tenant_hash)
);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON threat_intel_indicators TO noc_app_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON threat_intel_contributions TO noc_app_runtime;
    END IF;
END
$$;
