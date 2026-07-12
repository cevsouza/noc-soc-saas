-- 000014_alert_fingerprint.down.sql

DROP INDEX IF EXISTS idx_alerts_tenant_fingerprint;
ALTER TABLE alerts DROP COLUMN IF EXISTS fingerprint;
