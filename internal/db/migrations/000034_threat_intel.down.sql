-- 000034_threat_intel.down.sql
DROP TABLE IF EXISTS threat_intel_contributions;
DROP TABLE IF EXISTS threat_intel_indicators;
ALTER TABLE tenants DROP COLUMN IF EXISTS threat_intel_opt_in;
