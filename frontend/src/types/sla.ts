export type SLASeverity = 'fatal' | 'critical' | 'warning' | 'info';

// Matches internal/api/handler.go's SLASeverityBreakdown.
export interface SLASeverityBreakdown {
  severity: SLASeverity;
  target_minutes: number;
  count: number;
  resolved_count: number;
  average_tta: number;
  average_ttr: number;
  compliance_pct: number;
}

// Matches internal/api/handler.go's SLAExecutiveStats — the canonical MTTA/MTTR/SLA-compliance
// definition for incident response (severity-target-based, mirroring
// components/alerts/sla-countdown.tsx's thresholds). Always has 4 by_severity entries
// (fatal/critical/warning/info), even for severities with zero incidents in the period.
export interface SLAExecutiveStats {
  total_incidents: number;
  resolved_count: number;
  unresolved_count: number;
  average_tta: number;
  average_ttr: number;
  sla_compliance: number;
  by_severity: SLASeverityBreakdown[];
}
