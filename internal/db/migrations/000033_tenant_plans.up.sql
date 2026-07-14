-- 000033_tenant_plans.up.sql
--
-- Per-tenant billing plan + quota limits (Backlog B2 fatia 2). One row per tenant, keyed by
-- tenant_id. The MSSP operator (control-plane / platform admin) assigns each tenant a named plan
-- (free/starter/pro/enterprise or a custom override); the control-plane usage dashboard then renders
-- metered usage against these limits. This is the quota foundation the future Stripe billing
-- (B2 fatia 3) will price against — no external billing dependency yet.
--
-- Limits are INTEGER with the sentinel -1 meaning "unlimited" (enterprise). Alerts are counted over
-- the ~30-day usage window, which the app treats as one month for utilization display.

CREATE TABLE IF NOT EXISTS tenant_plans (
    tenant_id            UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    plan_name            VARCHAR(50) NOT NULL,
    max_alerts_per_month INTEGER NOT NULL,
    max_integrations     INTEGER NOT NULL,
    max_users            INTEGER NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE tenant_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_plans FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON tenant_plans
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_plans TO noc_app_runtime;
    END IF;
END
$$;
