// Mirrors internal/api/operational_stats.go — the tactical NOC/SOC KPI bundle that complements
// the SLA executive report (types/sla.ts). All values are computed over the same 30-day window
// (except triage_backlog, which is the current open count).

export interface TriageBacklog {
  triggered: number;
  acknowledged: number;
}

export interface NoiseRatio {
  total_alerts: number;
  distinct_incidents: number;
  ratio: number;
}

export interface OffenderCount {
  event_type: string;
  count: number;
}

export interface AutomationStats {
  soar_executed: number;
  soar_failed: number;
  response_executed: number;
  response_failed: number;
  estimated_hours_saved: number;
}

export interface MitreCount {
  tactic: string;
  count: number;
}

export interface SourceHeartbeat {
  type: string;
  last_seen_seconds_ago: number; // -1 = never reported
  silent: boolean;
}

// Closure quality (K1): how often a resolved alert had to be reopened within the window.
export interface ReworkStats {
  reopened: number;
  closed: number;
  reopen_rate_pct: number;
}

// SLA-breach escalations paged in the window.
export interface EscalationStats {
  sla_breaches: number;
}

export interface OperationalStats {
  window_days: number;
  triage_backlog: TriageBacklog;
  noise_ratio: NoiseRatio;
  top_offenders: OffenderCount[];
  automation: AutomationStats;
  rework: ReworkStats;
  escalations: EscalationStats;
  by_mitre: MitreCount[];
  source_health: SourceHeartbeat[];
}
