-- 000025_agent_metrics.up.sql
--
-- Raw SNMP metric samples pushed by the agent (slice 3): besides threshold-breach ALERTS (slice 2),
-- the agent now also reports every polled OID's value as a time-series sample, so the NOC console can
-- graph CPU/interface/etc over time. One row per (target, oid) per poll cycle.
--
-- Postgres is not a TSDB; this is a plain indexed table with a retention window (the metrics
-- enforcer deletes samples older than METRICS_RETENTION_DAYS). At larger scale, month-partitioning or
-- a dedicated TSDB is the path — documented, out of scope for v1.

CREATE TABLE IF NOT EXISTS agent_metrics (
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    target_id UUID,
    oid       VARCHAR(255) NOT NULL,
    label     VARCHAR(255) NOT NULL,
    value     DOUBLE PRECISION NOT NULL,
    ts        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_metrics_series ON agent_metrics (tenant_id, target_id, oid, ts DESC);
CREATE INDEX IF NOT EXISTS idx_agent_metrics_ts ON agent_metrics (ts);

ALTER TABLE agent_metrics ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_metrics FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON agent_metrics
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, DELETE ON agent_metrics TO noc_app_runtime;
    END IF;
END
$$;
