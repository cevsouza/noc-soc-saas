-- 000011_global_runbooks.down.sql
DROP POLICY IF EXISTS tenant_isolation_policy ON tenant_runbooks;

CREATE POLICY tenant_isolation_policy ON tenant_runbooks
    FOR ALL
    TO public
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

ALTER TABLE tenant_runbooks DROP COLUMN IF EXISTS is_global;
