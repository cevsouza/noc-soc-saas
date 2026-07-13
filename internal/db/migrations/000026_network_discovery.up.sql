-- 000026_network_discovery.up.sql
--
-- Active network discovery (topology slice A). Until now the topology tab only showed assets DERIVED
-- from the alert stream — a device that never fired an alert was invisible. This adds real, active
-- discovery: the agent sweeps configured CIDR ranges with an SNMP GET of sysName/sysDescr/sysObjectID
-- (outbound only, no inbound firewall rule), and every responder is recorded as a discovered device
-- with its identity (vendor + device type inferred from sysDescr/sysObjectID). This finds firewalls,
-- switches, APs and routers that are on the network but have never sent telemetry.
--
-- agent_discovery_targets: what to sweep. The SNMP community is a credential, so it is stored
-- ENCRYPTED per tenant (AES-GCM with the per-tenant derived key, same scheme as agent_snmp_targets)
-- and only decrypted when handed to the authenticated agent over TLS.
--
-- discovered_devices: the inventory the agent reports back. Unique per (tenant, ip); re-observing a
-- device refreshes last_seen/identity instead of inserting a duplicate.

CREATE TABLE IF NOT EXISTS agent_discovery_targets (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                VARCHAR(255) NOT NULL,
    cidr                VARCHAR(64) NOT NULL,
    port                INT NOT NULL DEFAULT 161,
    version             VARCHAR(8) NOT NULL DEFAULT '2c',
    community_encrypted BYTEA NOT NULL,
    community_nonce     BYTEA NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_discovery_targets_tenant ON agent_discovery_targets (tenant_id);

CREATE TABLE IF NOT EXISTS discovered_devices (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ip           VARCHAR(64) NOT NULL,
    sysname      VARCHAR(255) NOT NULL DEFAULT '',
    sysdescr     TEXT NOT NULL DEFAULT '',
    sysobjectid  VARCHAR(255) NOT NULL DEFAULT '',
    vendor       VARCHAR(64) NOT NULL DEFAULT '',
    device_type  VARCHAR(64) NOT NULL DEFAULT '',
    first_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, ip)
);
CREATE INDEX IF NOT EXISTS idx_discovered_devices_tenant ON discovered_devices (tenant_id, last_seen DESC);

ALTER TABLE agent_discovery_targets ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_discovery_targets FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON agent_discovery_targets
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

ALTER TABLE discovered_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE discovered_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON discovered_devices
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON agent_discovery_targets TO noc_app_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON discovered_devices TO noc_app_runtime;
    END IF;
END
$$;
