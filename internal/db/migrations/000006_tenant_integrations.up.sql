-- 000006_tenant_integrations.up.sql

CREATE TABLE tenant_integrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL, -- uptimekuma, zabbix, prometheus, wazuh, grafana, sentinel, loki, ssh, otlp, icinga, cloudwatch, azuremonitor, pagerduty, opsgenie, crowdstrike, paloalto, fortinet
    status VARCHAR(50) NOT NULL DEFAULT 'active', -- active, inactive
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, type, name)
);

-- Index for fast tenant lookup
CREATE INDEX idx_tenant_integrations_tenant ON tenant_integrations(tenant_id);

-- Enable Row-Level Security
ALTER TABLE tenant_integrations ENABLE ROW LEVEL SECURITY;

-- Establish tenant isolation policy
CREATE POLICY tenant_isolation_policy ON tenant_integrations
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());
