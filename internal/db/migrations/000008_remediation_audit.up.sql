-- 000008_remediation_audit.up.sql

-- 1. Create runbook execution logs table for security auditing
CREATE TABLE IF NOT EXISTS runbook_execution_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    runbook_id UUID NOT NULL REFERENCES tenant_runbooks(id) ON DELETE CASCADE,
    incident_id UUID NOT NULL, -- Logical link to alerts.id
    operator_name VARCHAR(255) NOT NULL,
    script TEXT NOT NULL,
    output TEXT NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Create incident comments / timeline table if not exists
CREATE TABLE IF NOT EXISTS incident_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL, -- Logical link to alerts.id
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    author VARCHAR(255) NOT NULL,
    comment TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 3. Row Level Security policies
ALTER TABLE runbook_execution_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_comments ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON runbook_execution_logs
    FOR ALL
    TO public
    USING (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid)
    WITH CHECK (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid);

CREATE POLICY tenant_isolation_policy ON incident_comments
    FOR ALL
    TO public
    USING (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid)
    WITH CHECK (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid);
