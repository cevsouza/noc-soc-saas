-- 000021_suppression_rules.up.sql
--
-- Configurable temporal suppression rules per tenant (Fase 3/3d). Until now the only suppression
-- was the fixed 5-minute fingerprint debounce. These rules let a tenant silence alerts matching a
-- field/pattern — optionally only within a time window (e.g. a maintenance window) — so planned or
-- known-noisy events don't create alerts/incidents.
--
-- match_field is one of: event_type, host, summary, source, severity. match_value is matched as a
-- case-insensitive substring. starts_at/ends_at bound an optional active window (both NULL = always
-- active while `active` is true). System-generated alerts (the connection watchdog) are never
-- suppressed — that gate lives in the worker.

CREATE TABLE IF NOT EXISTS tenant_suppression_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    match_field VARCHAR(20) NOT NULL,
    match_value TEXT NOT NULL,
    starts_at   TIMESTAMPTZ,
    ends_at     TIMESTAMPTZ,
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_suppression_tenant_active ON tenant_suppression_rules (tenant_id, active);

ALTER TABLE tenant_suppression_rules ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_suppression_rules FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_policy ON tenant_suppression_rules
    USING (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid)
    WITH CHECK (tenant_id = (SELECT NULLIF(current_setting('app.current_tenant_id', true), ''))::uuid);

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON tenant_suppression_rules TO noc_app_runtime;
    END IF;
END
$$;
