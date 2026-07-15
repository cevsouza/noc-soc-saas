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

// Sort options exposed to the operator for the alerts list. 'priority' (default) preserves the
// operational-console ordering; the others let the operator re-sort for a specific triage need.
export type AlertSortKey = 'priority' | 'recent' | 'oldest' | 'sla';

function byRecent(a: Alert, b: Alert): number {
  return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
}

function byOldest(a: Alert, b: Alert): number {
  return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
}

// Pure SLA urgency: soonest/most-overdue deadline first regardless of severity, severity as tiebreak.
function bySla(a: Alert, b: Alert): number {
  const deadline = slaDeadlineMs(a) - slaDeadlineMs(b);
  if (deadline !== 0) return deadline;
  return SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity];
}

// Resolves a sort key to its comparator. Falls back to priority for any unknown key.
export function alertComparator(key: AlertSortKey): (a: Alert, b: Alert) => number {
  switch (key) {
    case 'recent':
      return byRecent;
    case 'oldest':
      return byOldest;
    case 'sla':
      return bySla;
    case 'priority':
    default:
      return compareByPriority;
  }
}

// Time lens: a convenience narrowing of the open working set by alert age. 'all' is the safe default
// (never hides an open alert); narrower windows are opt-in and the console surfaces a count of what
// they hide so danger is never silently dropped.
export type TimeLens = 'all' | '1h' | '24h' | '7d';

export const TIME_LENS_MINUTES: Record<Exclude<TimeLens, 'all'>, number> = {
  '1h': 60,
  '24h': 1440,
  '7d': 10080,
};

// True when the alert falls inside the lens window (or the lens is 'all'). `now` is injectable for
// deterministic testing.
export function withinLens(alert: Pick<Alert, 'created_at'>, lens: TimeLens, now: number = Date.now()): boolean {
  if (lens === 'all') return true;
  return new Date(alert.created_at).getTime() >= now - TIME_LENS_MINUTES[lens] * 60 * 1000;
}
