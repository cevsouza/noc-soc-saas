-- 000019_incidents.down.sql
ALTER TABLE alerts DROP COLUMN IF EXISTS incident_id;
DROP TABLE IF EXISTS incidents;
