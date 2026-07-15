-- Incident disposition (K5): the analyst's verdict on an incident — was it a real problem
-- (true_positive), a false alarm (false_positive), or expected/benign activity? NULL means
-- unclassified. This is the input to the false-positive-rate KPI, the SOC quality metric the
-- platform was missing. Nullable, no backfill: existing incidents stay unclassified until triaged.
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS disposition VARCHAR(20);

-- Speeds up the disposition breakdown query (counts by disposition within the tenant/window).
CREATE INDEX IF NOT EXISTS idx_incidents_tenant_disposition ON incidents (tenant_id, disposition) WHERE disposition IS NOT NULL;
