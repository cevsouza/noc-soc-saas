-- 000016_response_actions.up.sql
--
-- Outbound response actions (Fase 5 fatia 4): the platform can now request vendor-native
-- containment/blocking — block a source IP on a firewall (Palo Alto, Fortinet) or contain a
-- host on the EDR (CrowdStrike). Every such action changes network/endpoint state, so it is
-- ALWAYS gated by human approval, mirroring the runbook_approval_requests flow: an operator
-- files a request, an operator/admin approves, and only then does the vendor API call fire.
-- This table is the queue + audit trail for those actions.

CREATE TABLE IF NOT EXISTS response_action_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    -- Nullable: an action can be tied to an alert (block this alert's source IP) or filed
    -- standalone (an analyst blocking an IOC by hand).
    incident_id UUID,
    integration_type VARCHAR(50) NOT NULL,  -- crowdstrike, paloalto, fortinet
    action_type VARCHAR(50) NOT NULL,       -- block_ip, unblock_ip, contain_host, lift_containment
    target VARCHAR(255) NOT NULL,           -- IP address (firewall) or device/host id (EDR)
    status VARCHAR(50) NOT NULL DEFAULT 'pending', -- pending, approved, rejected, failed
    reason TEXT NOT NULL DEFAULT '',
    requested_by VARCHAR(255) NOT NULL DEFAULT 'system',
    approved_by UUID REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    output TEXT NOT NULL DEFAULT '',        -- vendor response or execution error, filled on approve
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE response_action_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE response_action_requests FORCE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation_policy ON response_action_requests
    FOR ALL
    TO public
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

CREATE INDEX IF NOT EXISTS idx_response_action_tenant_status ON response_action_requests(tenant_id, status);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON response_action_requests TO noc_app_runtime;
    END IF;
END
$$;
