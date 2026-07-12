-- 000014_alert_fingerprint.up.sql
--
-- Adds a content-hash fingerprint column to `alerts` for dedupe auditing/search. This is NOT
-- a uniqueness constraint (the partitioned table + legitimately-repeating content makes that
-- more complexity than this phase needs) — the actual dedupe gate is a Redis key with a TTL
-- (internal/worker/worker.go, internal/worker/fingerprint.go); this column just makes the
-- fingerprint a first-class, queryable field on the row itself.

ALTER TABLE alerts ADD COLUMN IF NOT EXISTS fingerprint VARCHAR(64);

CREATE INDEX IF NOT EXISTS idx_alerts_tenant_fingerprint ON alerts(tenant_id, fingerprint);
