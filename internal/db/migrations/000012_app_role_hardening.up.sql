-- 000012_app_role_hardening.up.sql
--
-- Defense-in-depth for tenant isolation:
-- 1. FORCE ROW LEVEL SECURITY on every tenant-scoped table so RLS policies apply even to
--    the table owner (by default PostgreSQL exempts the owner from its own RLS policies,
--    and the owner is exactly the role this application uses to run migrations).
-- 2. Provision a non-superuser runtime role ("noc_app_runtime") so the application can run
--    as a role that is NOT the table owner, which is what makes FORCE ROW LEVEL SECURITY
--    meaningful in practice. If the connected PostgreSQL user lacks CREATEROLE privilege
--    (common on some managed database providers), this block is skipped gracefully — FORCE
--    ROW LEVEL SECURITY above still helps as long as the app does not connect as a superuser
--    or as the table owner.
--
-- The role's password is intentionally NOT set here — see internal/db/connection.go
-- (SetupAppRuntimeRole), which rotates it at boot from the APP_DB_PASSWORD environment
-- variable, so no secret is committed to version control.

ALTER TABLE tenant_users FORCE ROW LEVEL SECURITY;
ALTER TABLE devices FORCE ROW LEVEL SECURITY;
ALTER TABLE alerts FORCE ROW LEVEL SECURITY;
ALTER TABLE self_healing_actions FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_api_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_vault FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_integrations FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_runbooks FORCE ROW LEVEL SECURITY;
ALTER TABLE runbook_execution_logs FORCE ROW LEVEL SECURITY;
ALTER TABLE incident_comments FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_mapping_rules FORCE ROW LEVEL SECURITY;
ALTER TABLE shift_handovers FORCE ROW LEVEL SECURITY;

-- Create the runtime role if the connected user has privilege to do so; skip silently
-- (with a NOTICE) otherwise, so this migration never blocks the rest of the pipeline.
DO $$
BEGIN
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
            CREATE ROLE noc_app_runtime WITH LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
        END IF;
    EXCEPTION WHEN insufficient_privilege THEN
        RAISE NOTICE 'Insufficient privilege to create role noc_app_runtime — skipping second-layer role separation. Tenant isolation still relies on FORCE ROW LEVEL SECURITY plus explicit tenant_id filters in application code.';
    END;
END
$$;

-- Grant the minimum privileges the application needs, only if the role exists.
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        EXECUTE format('GRANT CONNECT ON DATABASE %I TO noc_app_runtime', current_database());
        GRANT USAGE ON SCHEMA public TO noc_app_runtime;
        GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO noc_app_runtime;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO noc_app_runtime;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO noc_app_runtime;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO noc_app_runtime;
    END IF;
END
$$;
