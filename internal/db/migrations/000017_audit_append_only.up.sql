-- 000017_audit_append_only.up.sql
--
-- Make audit_logs write-once for the application. Sensitive-action audit records must not be
-- alterable or erasable through the running app, so a compromised app or a malicious insider using
-- it cannot cover their tracks.
--
-- Enforcement is twofold:
--   1. A trigger makes every row immutable (no UPDATE for anyone — audit content is never edited)
--      and blocks DELETE specifically for the application runtime role (noc_app_runtime).
--   2. REVOKE UPDATE/DELETE from noc_app_runtime as a privilege-level backstop.
--
-- DELETE is intentionally still allowed for the table owner / a superuser, so legitimate
-- privileged operations keep working: tenant offboarding (audit_logs.tenant_id has ON DELETE
-- CASCADE) and future data-retention purges. Those paths run as the owner, not as noc_app_runtime.

CREATE OR REPLACE FUNCTION audit_logs_append_only() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'audit_logs rows are immutable (UPDATE is not permitted)';
    END IF;
    IF TG_OP = 'DELETE' AND current_user = 'noc_app_runtime' THEN
        RAISE EXCEPTION 'audit_logs is append-only for the application role (DELETE is not permitted)';
    END IF;
    RETURN CASE WHEN TG_OP = 'DELETE' THEN OLD ELSE NEW END;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_audit_logs_append_only ON audit_logs;
CREATE TRIGGER trg_audit_logs_append_only
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_append_only();

-- Privilege-level backstop: only if the runtime role exists (it is provisioned best-effort in
-- 000012; on managed providers without CREATEROLE it may be absent, in which case the trigger
-- above still enforces immutability).
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        REVOKE UPDATE, DELETE ON audit_logs FROM noc_app_runtime;
    END IF;
END
$$;
