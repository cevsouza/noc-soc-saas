-- MTTD / time-to-detect (K4): alerts.created_at is set to the event's ORIGIN time — the timestamp
-- the connector parses from the source payload (Prometheus startsAt, Zabbix clock, Wazuh/Azure/etc).
-- To measure detection delay we need a separate, IMMUTABLE ingestion timestamp: ingested_at defaults
-- to NOW() at insert and is never updated, so (ingested_at - created_at) is the time-to-detect.
-- Existing rows backfill to the migration time; historical MTTD is not reconstructable (documented).
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS ingested_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
