-- 000007_incident_remediation.up.sql

-- 1. Add time tracking and AI diagnostics fields to alerts
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS acknowledged_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ai_diagnostic TEXT DEFAULT NULL;

-- 2. Create Runbooks table for remote scripts execution
CREATE TABLE IF NOT EXISTS tenant_runbooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    trigger_rule VARCHAR(255) NOT NULL,
    script TEXT NOT NULL,
    vault_key_host VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Row Level Security policies
ALTER TABLE tenant_runbooks ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON tenant_runbooks
    FOR ALL
    TO public
    USING (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid)
    WITH CHECK (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid);
