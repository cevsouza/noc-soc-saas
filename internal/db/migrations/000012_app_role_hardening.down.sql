-- 000012_app_role_hardening.down.sql

ALTER TABLE tenant_users NO FORCE ROW LEVEL SECURITY;
ALTER TABLE devices NO FORCE ROW LEVEL SECURITY;
ALTER TABLE alerts NO FORCE ROW LEVEL SECURITY;
ALTER TABLE self_healing_actions NO FORCE ROW LEVEL SECURITY;
ALTER TABLE audit_logs NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_api_keys NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_vault NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_integrations NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_runbooks NO FORCE ROW LEVEL SECURITY;
ALTER TABLE runbook_execution_logs NO FORCE ROW LEVEL SECURITY;
ALTER TABLE incident_comments NO FORCE ROW LEVEL SECURITY;
ALTER TABLE tenant_mapping_rules NO FORCE ROW LEVEL SECURITY;
ALTER TABLE shift_handovers NO FORCE ROW LEVEL SECURITY;

-- The noc_app_runtime role is intentionally not dropped automatically here (it may own
-- granted privileges relied upon elsewhere). Drop manually if truly needed:
-- DROP ROLE IF EXISTS noc_app_runtime;
