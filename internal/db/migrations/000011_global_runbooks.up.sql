-- 000011_global_runbooks.up.sql

-- 1. Add is_global column to tenant_runbooks table
ALTER TABLE tenant_runbooks ADD COLUMN IF NOT EXISTS is_global BOOLEAN DEFAULT FALSE;

-- 2. Update RLS policy to allow reading global runbooks
DROP POLICY IF EXISTS tenant_isolation_policy ON tenant_runbooks;

CREATE POLICY tenant_isolation_policy ON tenant_runbooks
    FOR ALL
    TO public
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid OR is_global = TRUE)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);
