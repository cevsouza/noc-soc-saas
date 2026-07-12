-- 000015_alert_partition_autocreate.up.sql
--
-- URGENT FIX: `alerts` is range-partitioned by created_at (see 000001_init_schema.up.sql), but
-- only static partitions through 2026-07-31 were ever created, with no job to add more. Once
-- the current month rolls over, INSERTs into alerts start failing outright (no partition
-- covers the new created_at range). This adds a Postgres function that creates a given
-- month's partition if it doesn't already exist, runs it once here as a catch-up for the next
-- several months, and is also called from cmd/noc-api/main.go on every boot (current month +
-- next month) so partitions never run out again — no pg_cron needed, and Railway's managed
-- Postgres typically doesn't offer that extension anyway.

CREATE OR REPLACE FUNCTION create_alerts_partition_if_needed(target_month date)
RETURNS void AS $$
DECLARE
    partition_name text;
    start_date date;
    end_date date;
BEGIN
    start_date := date_trunc('month', target_month)::date;
    end_date := (start_date + interval '1 month')::date;
    partition_name := 'alerts_y' || to_char(start_date, 'YYYY') || 'm' || to_char(start_date, 'MM');

    IF NOT EXISTS (
        SELECT 1 FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relname = partition_name AND n.nspname = 'public'
    ) THEN
        EXECUTE format(
            'CREATE TABLE %I PARTITION OF alerts FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
    END IF;
END;
$$ LANGUAGE plpgsql;

-- Catch-up: ensure the current month plus the next 6 months all have partitions already, so
-- there's headroom even if the app doesn't boot for a while.
DO $$
DECLARE
    i integer;
BEGIN
    FOR i IN 0..6 LOOP
        PERFORM create_alerts_partition_if_needed((date_trunc('month', now()) + (i || ' months')::interval)::date);
    END LOOP;
END;
$$;
