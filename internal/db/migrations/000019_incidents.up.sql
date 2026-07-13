-- 000019_incidents.up.sql
--
-- Separate "alert" (a raw, correlated event) from "incident" (the single investigation that groups
-- the recurring alerts of the same underlying problem). Until now the two were conflated: the
-- app used an alert's id wherever it said "incident_id". This introduces a real incidents table and
-- links each alert to the incident it belongs to.
--
-- This migration is intentionally ADDITIVE and non-breaking: alerts.incident_id is nullable, and the
-- existing alert-level acknowledge/resolve/comment/approval flows keep working unchanged. Migrating
-- those flows to operate at the incident level is a later slice.

CREATE TABLE IF NOT EXISTS incidents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    fingerprint  VARCHAR(64) NOT NULL,
    title        TEXT NOT NULL,
    severity     VARCHAR(20) NOT NULL,          -- worst severity observed across grouped alerts
    status       VARCHAR(20) NOT NULL DEFAULT 'open', -- open, acknowledged, resolved
    alert_count  INT NOT NULL DEFAULT 1,
    first_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    resolved_at  TIMESTAMPTZ
);

-- One open incident per (tenant, fingerprint): recurring alerts of the same problem attach to the
-- same open investigation instead of spawning a new one. A tenant may still have a historical
-- resolved incident plus a new open one for the same fingerprint, so the uniqueness is partial.
CREATE UNIQUE INDEX IF NOT EXISTS idx_incidents_open_fingerprint
    ON incidents (tenant_id, fingerprint) WHERE status <> 'resolved';
CREATE INDEX IF NOT EXISTS idx_incidents_tenant_status ON incidents (tenant_id, status, last_seen DESC);

ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON incidents
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

-- Link alerts to their incident. Nullable, no FK constraint: alerts is a partitioned table and a
-- cross-table FK from every partition adds friction with little benefit here — integrity is
-- enforced by application logic inside the tenant RLS transaction (same pragmatic choice as
-- device_id references elsewhere).
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS incident_id UUID;
CREATE INDEX IF NOT EXISTS idx_alerts_tenant_incident ON alerts (tenant_id, incident_id);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON incidents TO noc_app_runtime;
    END IF;
END
$$;
