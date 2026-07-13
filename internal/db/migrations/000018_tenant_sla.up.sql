-- 000018_tenant_sla.up.sql
--
-- Per-tenant SLA targets. Until now the incident-response SLA targets (MTTA/MTTR per severity)
-- were a single hardcoded set (a Go map + a SQL CTE + a Python map). This table lets each tenant
-- override them per contract; the SLA engine reads this table and falls back to the built-in
-- defaults for any severity a tenant hasn't customized.
--
-- escalation_policy_id is forward-looking (a dedicated escalation-policy engine is a later slice);
-- it is a plain nullable UUID with no FK yet so the column exists for the contract described in the
-- roadmap without depending on a table that isn't built.

CREATE TABLE IF NOT EXISTS tenant_sla (
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    severity             VARCHAR(20) NOT NULL,
    mtta_target_minutes  NUMERIC NOT NULL CHECK (mtta_target_minutes > 0),
    mttr_target_minutes  NUMERIC NOT NULL CHECK (mttr_target_minutes > 0),
    escalation_policy_id UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, severity)
);

ALTER TABLE tenant_sla ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_sla FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON tenant_sla
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_sla TO noc_app_runtime;
    END IF;
END
$$;
