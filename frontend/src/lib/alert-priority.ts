import type { Alert, AlertSeverity } from '@/types';

// Operational-console ordering primitives. The console shows the actionable working set (open
// alerts) ranked by urgency, not a flat recency feed — a 26h-old still-open critical must sit above
// a fresh info. Risk at the alert level is proxied by severity; SLA burn refines it within a band.

// Higher = more urgent. Used as the primary sort key and to rank severities consistently.
export const SEVERITY_RANK: Record<AlertSeverity, number> = {
  fatal: 4,
  critical: 3,
  warning: 2,
  info: 1,
};

// Minutes-to-SLA-breach target per severity. Canonical source mirrored by sla-countdown.tsx (which
// imports these) and the backend tenant_sla defaults — keep the three in sync if they ever change.
export const SLA_TARGET_MINUTES: Record<AlertSeverity, number> = {
  fatal: 15,
  critical: 30,
  warning: 120,
  info: 480,
};

// An alert is "open" (belongs in the operational console) unless it has been resolved or suppressed.
export function isOpen(alert: Pick<Alert, 'status'>): boolean {
  return alert.status !== 'resolved' && alert.status !== 'suppressed';
}

// Absolute SLA deadline in epoch ms: when this alert breaches its severity target. Earlier (or
// already past) = more urgent.
export function slaDeadlineMs(alert: Pick<Alert, 'created_at' | 'severity'>): number {
  return new Date(alert.created_at).getTime() + SLA_TARGET_MINUTES[alert.severity] * 60 * 1000;
}

// Sort comparator for the operational console: highest severity first (risk), then soonest/most
// overdue SLA deadline within a severity band (SLA burn). Newest wins only as a final tiebreak.
export function compareByPriority(a: Alert, b: Alert): number {
  const rank = SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity];
  if (rank !== 0) return rank;
  const deadline = slaDeadlineMs(a) - slaDeadlineMs(b);
  if (deadline !== 0) return deadline;
  return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
}
