-- 000003_api_keys.up.sql

-- Create tenant_api_keys table
CREATE TABLE tenant_api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(64) UNIQUE NOT NULL, -- SHA-256 hash (64 hex chars)
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

-- Index for instant key resolution
CREATE INDEX idx_api_keys_hash ON tenant_api_keys(key_hash);
CREATE INDEX idx_api_keys_tenant ON tenant_api_keys(tenant_id);

-- Enable Row-Level Security
ALTER TABLE tenant_api_keys ENABLE ROW LEVEL SECURITY;

-- Establish tenant isolation policy
CREATE POLICY tenant_isolation_policy ON tenant_api_keys
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());
