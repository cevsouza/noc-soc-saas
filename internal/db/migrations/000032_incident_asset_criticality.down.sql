-- 000032_incident_asset_criticality.down.sql
ALTER TABLE incidents DROP COLUMN IF EXISTS asset_criticality;
