-- 000013_soar_approval.up.sql
--
-- Auto-remediation (SOAR) currently executes real SSH commands against tenant
-- infrastructure the moment a critical/fatal alert fires, with no human review. This
-- migration adds the schema needed to gate that: runbooks must be explicitly marked
-- "is_safe" to auto-execute, and every other auto-trigger candidate becomes an approval
-- request instead of an immediate execution.

ALTER TABLE tenant_runbooks ADD COLUMN IF NOT EXISTS is_safe BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS runbook_approval_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    runbook_id UUID NOT NULL REFERENCES tenant_runbooks(id) ON DELETE CASCADE,
    incident_id UUID NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, approved, rejected
    requested_by VARCHAR(255) NOT NULL DEFAULT 'system',
    approved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE runbook_approval_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE runbook_approval_requests FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON runbook_approval_requests
    FOR ALL
    TO public
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

CREATE INDEX IF NOT EXISTS idx_runbook_approval_tenant_status ON runbook_approval_requests(tenant_id, status);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON runbook_approval_requests TO noc_app_runtime;
    END IF;
END
$$;
