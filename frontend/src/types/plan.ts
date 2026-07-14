// Billing plans + quotas (Backlog B2 fatia 2). Mirrors internal/api/plans.go.
// PLAN_NAMES must stay in sync with the Go planPresets map (names only). -1 limits mean unlimited.

export interface TenantPlan {
  plan_name: string;
  max_alerts_per_month: number;
  max_integrations: number;
  max_users: number;
}

export const PLAN_NAMES = ['free', 'starter', 'pro', 'enterprise'] as const;
export type PlanName = (typeof PLAN_NAMES)[number];

export const UNLIMITED = -1;

// formatLimit renders a quota limit for display: -1/0 → the infinity sign.
export function formatLimit(limit: number | undefined): string {
  if (limit === undefined || limit <= 0) return '∞';
  return new Intl.NumberFormat('pt-BR').format(limit);
}

// utilizationPct mirrors the Go helper: used/limit as a percentage, 0 for unlimited/unset.
export function utilizationPct(used: number, limit: number | undefined): number {
  if (!limit || limit <= 0) return 0;
  return (used / limit) * 100;
}
