-- 000023_agent.up.sql
--
-- NOC/SOC agent foundation. A lightweight agent runs on the customer's network and talks to the SaaS
-- ONLY outbound over 443 (enroll, poll config, push events) — no inbound firewall rules. This adds:
--   * agent_enrollment_tokens: one-time, short-lived credentials a tenant admin generates so an agent
--     can self-enroll and receive a tenant API key. Only the sha256 hash is stored; the plaintext is
--     shown once. The enroll lookup is by the globally-unique token hash on the privileged pool (same
--     pattern as login/API-key auth), so it needs no tenant context.
--   * agents: one row per enrolled agent, for liveness (last_seen) and the console.
--
-- Both tables get FORCE RLS + tenant-isolation policy (consistent with every tenant table); the
-- tenant-scoped paths (generate token, list agents) run inside the tenant RLS context, while the
-- unauthenticated enroll path reads by token hash on the privileged pool.

CREATE TABLE IF NOT EXISTS agent_enrollment_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS agents (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       VARCHAR(255) NOT NULL,
    api_key_id UUID REFERENCES tenant_api_keys(id) ON DELETE SET NULL,
    version    VARCHAR(64),
    last_seen  TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agents_tenant ON agents (tenant_id);

ALTER TABLE agent_enrollment_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_enrollment_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON agent_enrollment_tokens
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE agents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON agents
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON agent_enrollment_tokens TO noc_app_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON agents TO noc_app_runtime;
    END IF;
END
$$;
