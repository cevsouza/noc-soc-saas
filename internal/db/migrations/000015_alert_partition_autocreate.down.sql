-- 000015_alert_partition_autocreate.down.sql
--
-- Partition tables created by the function are intentionally left in place (they may already
-- hold data) — only the function itself is removed.

DROP FUNCTION IF EXISTS create_alerts_partition_if_needed(date);
