-- 000027_discovered_links.up.sql
--
-- Physical neighbourhood (topology slice B). Slice A inventoried devices; this records the real
-- host-to-host EDGES between them, discovered by the agent walking each device's LLDP (IEEE 802.1AB)
-- and CDP (Cisco) neighbour tables over SNMP. Each row is one directed adjacency: local device port X
-- sees a remote neighbour (identified by chassis id / sysName / remote port). Slice C stitches these
-- into the topology graph.
--
-- Unique per (tenant, local_ip, local_port, remote_chassis_id, remote_port_id) so re-walking a device
-- refreshes last_seen instead of duplicating the edge. remote_chassis_id/remote_port_id are the stable
-- neighbour identity from LLDP; for CDP (no chassis id) we store the device id there.

CREATE TABLE IF NOT EXISTS discovered_links (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    local_ip          VARCHAR(64) NOT NULL,
    local_port        VARCHAR(128) NOT NULL DEFAULT '',
    remote_sysname    VARCHAR(255) NOT NULL DEFAULT '',
    remote_chassis_id VARCHAR(255) NOT NULL DEFAULT '',
    remote_port_id    VARCHAR(255) NOT NULL DEFAULT '',
    protocol          VARCHAR(8) NOT NULL DEFAULT 'lldp',
    first_seen        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, local_ip, local_port, remote_chassis_id, remote_port_id)
);
CREATE INDEX IF NOT EXISTS idx_discovered_links_tenant ON discovered_links (tenant_id, last_seen DESC);

ALTER TABLE discovered_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE discovered_links FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON discovered_links
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON discovered_links TO noc_app_runtime;
    END IF;
END
$$;
