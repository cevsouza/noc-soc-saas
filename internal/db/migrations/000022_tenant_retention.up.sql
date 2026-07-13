-- 000022_tenant_retention.up.sql
--
-- Per-tenant, OPT-IN data retention for alerts (Fase 5). A tenant with NO row here keeps its alerts
-- forever — the safest possible default, so a deploy never silently deletes anyone's data. A row
-- sets how many days of alerts to keep; a retention-enforcement worker deletes alerts older than
-- that window. A hard 30-day floor is enforced BOTH by the CHECK here and again in application code
-- (defense-in-depth), so no configuration can ever delete data younger than 30 days.
--
-- Note: the alerts table is partitioned by month and shared across tenants, so retention is applied
-- as a tenant-scoped row DELETE (contract-precise) rather than a partition drop (which would hit all
-- tenants). Dropping whole old partitions remains the scalable platform-wide floor, handled
-- separately.

CREATE TABLE IF NOT EXISTS tenant_retention (
    tenant_id             UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    alerts_retention_days INT NOT NULL CHECK (alerts_retention_days >= 30),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE tenant_retention ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_retention FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON tenant_retention
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_retention TO noc_app_runtime;
    END IF;
END
$$;
