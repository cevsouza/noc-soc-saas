-- 000004_tenant_vault.up.sql

CREATE TABLE tenant_vault (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    secret_key VARCHAR(255) NOT NULL,
    encrypted_value BYTEA NOT NULL,
    nonce BYTEA NOT NULL, -- 12 bytes nonce for AES-GCM
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, secret_key)
);

-- Index for tenant lookup
CREATE INDEX idx_tenant_vault_tenant ON tenant_vault(tenant_id);

-- Enable Row-Level Security
ALTER TABLE tenant_vault ENABLE ROW LEVEL SECURITY;

-- Establish tenant isolation policy
CREATE POLICY tenant_isolation_policy ON tenant_vault
    FOR ALL
    USING (tenant_id = get_current_tenant_id())
    WITH CHECK (tenant_id = get_current_tenant_id());
