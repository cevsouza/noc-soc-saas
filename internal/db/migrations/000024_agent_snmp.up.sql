-- 000024_agent_snmp.up.sql
--
-- SNMP collection targets for the NOC/SOC agent (slice 2). A tenant admin registers network devices
-- (firewall/switch/AP/router) with the OIDs to poll and thresholds to evaluate; the agent pulls this
-- via /agent/config and, on a threshold breach, emits an alert through /agent/events (outbound 443).
--
-- The SNMP community is a credential, so it is stored ENCRYPTED per tenant (AES-GCM with the
-- per-tenant derived key, same scheme as the vault) and only decrypted when handed to the
-- authenticated agent over TLS. `checks` is a JSON array of {oid,label,comparison,threshold,severity}.

CREATE TABLE IF NOT EXISTS agent_snmp_targets (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                VARCHAR(255) NOT NULL,
    host                VARCHAR(255) NOT NULL,
    port                INT NOT NULL DEFAULT 161,
    version             VARCHAR(8) NOT NULL DEFAULT '2c',
    community_encrypted BYTEA NOT NULL,
    community_nonce     BYTEA NOT NULL,
    checks              JSONB NOT NULL DEFAULT '[]',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_snmp_targets_tenant ON agent_snmp_targets (tenant_id);

ALTER TABLE agent_snmp_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_snmp_targets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON agent_snmp_targets
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON agent_snmp_targets TO noc_app_runtime;
    END IF;
END
$$;
