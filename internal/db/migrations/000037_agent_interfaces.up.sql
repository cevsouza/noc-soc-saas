-- 000037_agent_interfaces.up.sql
--
-- Physical link utilization (topology slice T-D). The agent walks each SNMP-monitored device's
-- ifXTable/ifTable and computes per-interface throughput (in/out bps from the HC octet counters),
-- link speed and oper status. The topology graph joins this to the LLDP/CDP edges by (device_ip,
-- ifindex) — the link's local_port is the local ifIndex — to color/thicken edges by real utilization.
-- Latest sample per interface only (upsert); this is not time-series.

CREATE TABLE IF NOT EXISTS agent_interfaces (
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    device_ip   VARCHAR(64) NOT NULL,
    ifindex     VARCHAR(16) NOT NULL,
    ifname      VARCHAR(128) NOT NULL DEFAULT '',
    oper_status VARCHAR(16) NOT NULL DEFAULT '',
    in_bps      BIGINT NOT NULL DEFAULT 0,
    out_bps     BIGINT NOT NULL DEFAULT 0,
    speed_bps   BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, device_ip, ifindex)
);
CREATE INDEX IF NOT EXISTS idx_agent_interfaces_device ON agent_interfaces (tenant_id, device_ip);

ALTER TABLE agent_interfaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_interfaces FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON agent_interfaces
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON agent_interfaces TO noc_app_runtime;
    END IF;
END
$$;
