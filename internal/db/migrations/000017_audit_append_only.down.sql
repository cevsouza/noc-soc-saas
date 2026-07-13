-- 000017_audit_append_only.down.sql
-- Reverses the append-only enforcement on audit_logs.

DROP TRIGGER IF EXISTS trg_audit_logs_append_only ON audit_logs;
DROP FUNCTION IF EXISTS audit_logs_append_only();

DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_roles WHERE rolname = 'noc_app_runtime') THEN
        GRANT UPDATE, DELETE ON audit_logs TO noc_app_runtime;
    END IF;
END
$$;
