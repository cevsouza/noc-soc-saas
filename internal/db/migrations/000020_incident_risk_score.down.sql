-- 000020_incident_risk_score.down.sql
ALTER TABLE incidents DROP COLUMN IF EXISTS risk_score;
