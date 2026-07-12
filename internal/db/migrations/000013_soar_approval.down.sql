-- 000013_soar_approval.down.sql

DROP TABLE IF EXISTS runbook_approval_requests;
ALTER TABLE tenant_runbooks DROP COLUMN IF EXISTS is_safe;
