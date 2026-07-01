-- 000010_msp_architecture.up.sql

-- 1. Create MSP organization table
CREATE TABLE IF NOT EXISTS msp_organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    domain VARCHAR(100) UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed initial msp organization
INSERT INTO msp_organizations (name, domain)
VALUES ('IT Fácil S.A.', 'itfacil.com.br')
ON CONFLICT (domain) DO NOTHING;

-- 2. Alter tenants to associate with msp
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS msp_id UUID REFERENCES msp_organizations(id) ON DELETE SET NULL;

-- Associate existing tenants to first MSP organization
UPDATE tenants SET msp_id = (SELECT id FROM msp_organizations LIMIT 1) WHERE msp_id IS NULL;

-- 3. Create mapping rules table
CREATE TABLE IF NOT EXISTS tenant_mapping_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    source_tool VARCHAR(50) NOT NULL,
    source_field VARCHAR(100) NOT NULL,
    source_value VARCHAR(100) NOT NULL,
    normalized_value VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, source_tool, source_field, source_value)
);

-- Seed some default mapping rules (Zabbix -> SOC/NOC)
INSERT INTO tenant_mapping_rules (tenant_id, source_tool, source_field, source_value, normalized_value)
SELECT id, 'zabbix', 'severity', 'Disaster', 'fatal' FROM tenants
ON CONFLICT DO NOTHING;

INSERT INTO tenant_mapping_rules (tenant_id, source_tool, source_field, source_value, normalized_value)
SELECT id, 'zabbix', 'severity', 'High', 'critical' FROM tenants
ON CONFLICT DO NOTHING;

-- 4. Create shift handovers table
CREATE TABLE IF NOT EXISTS shift_handovers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    msp_id UUID NOT NULL REFERENCES msp_organizations(id) ON DELETE CASCADE,
    outgoing_operator_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    incoming_operator_id UUID REFERENCES users(id) ON DELETE SET NULL,
    shift_summary TEXT NOT NULL,
    pending_alerts_count INT DEFAULT 0,
    status VARCHAR(50) DEFAULT 'pending', -- pending, acknowledged
    acknowledged_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Enable RLS on mapping rules and shift handovers
ALTER TABLE tenant_mapping_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE shift_handovers ENABLE ROW LEVEL SECURITY;

-- Create RLS policies
CREATE POLICY tenant_isolation_policy ON tenant_mapping_rules
    FOR ALL
    TO public
    USING (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid)
    WITH CHECK (tenant_id = (SELECT current_setting('app.current_tenant_id', true))::uuid);

-- Shift handover uses msp_id mapping, but for simplicity of tenant users access we check tenant_users
CREATE POLICY msp_isolation_policy ON shift_handovers
    FOR ALL
    TO public
    USING (msp_id IN (
        SELECT msp_id FROM tenants WHERE id = (SELECT current_setting('app.current_tenant_id', true))::uuid
    ))
    WITH CHECK (msp_id IN (
        SELECT msp_id FROM tenants WHERE id = (SELECT current_setting('app.current_tenant_id', true))::uuid
    ));
