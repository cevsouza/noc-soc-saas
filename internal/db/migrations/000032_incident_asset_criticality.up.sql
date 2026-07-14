-- 000032_incident_asset_criticality.up.sql
--
-- Fold asset criticality into the incident risk score (Backlog B1, MSSP R3). Migration 000020 left
-- asset criticality as a documented future input to the dynamic risk score; the CMDB (topology slice
-- T2, `assets.business_criticality`) now provides it. The worker resolves the alerting host to a
-- managed asset (by identifier or alias) and records its criticality here so the elevated risk score
-- is explainable in the UI ("this incident hits a critical asset"), not just a silently higher number.
--
-- Nullable: an incident whose host isn't a managed asset simply has no criticality (scores as before).

ALTER TABLE incidents ADD COLUMN IF NOT EXISTS asset_criticality VARCHAR(16);
