-- 000020_incident_risk_score.up.sql
--
-- Dynamic risk score (0-100) for incidents (Fase 3/3c). Replaces "risk = static severity" with a
-- score that combines the worst severity of the grouped alerts with recurrence (how many alerts the
-- incident has accumulated — a proxy for the problem's persistence / tenant history). Computed by
-- the worker as alerts are grouped (see internal/worker/incidents.go's computeRiskScore).
--
-- Not yet folded in (documented data gaps, future enhancements): asset criticality (needs a device
-- criticality registry) and threat-intel confidence (needs a real IOC feed with scores).

ALTER TABLE incidents ADD COLUMN IF NOT EXISTS risk_score INT NOT NULL DEFAULT 0;
