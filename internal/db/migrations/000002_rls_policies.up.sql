-- 000002_rls_policies.up.sql

-- Helper function to retrieve the tenant ID from the session configuration
CREATE OR REPLACE FUNCTION get_current_tenant_id()
RETURNS UUID AS $$
BEGIN
    RETURN NULLIF(current_setting('app.current_tenant_id', true), '')::UUID;
EXCEPTION
    WHEN OTHERS THEN
        RETURN NULL;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Enable Row-Level Security on all tenant-owned tables
ALTER TABLE tenant_users ENABLE ROW LEVEL SECURITY;
ALTER TABLE devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE self_healing_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;

-- 1. Policies for tenant_users
CREATE POLICY tenant_isolation_policy ON tenant_users
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());

-- 2. Policies for devices
CREATE POLICY tenant_isolation_policy ON devices
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());

-- 3. Policies for alerts (PostgreSQL propagates these to all child partitions)
CREATE POLICY tenant_isolation_policy ON alerts
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());

-- 4. Policies for self_healing_actions
CREATE POLICY tenant_isolation_policy ON self_healing_actions
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());

-- 5. Policies for audit_logs
CREATE POLICY tenant_isolation_policy ON audit_logs
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());
