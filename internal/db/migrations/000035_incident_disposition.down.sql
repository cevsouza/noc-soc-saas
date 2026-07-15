DROP INDEX IF EXISTS idx_incidents_tenant_disposition;
ALTER TABLE incidents DROP COLUMN IF EXISTS disposition;
