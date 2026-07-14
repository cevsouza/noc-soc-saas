// Usage metering (Fase 6 / Backlog B2). Mirrors internal/api/usage.go.
// Control-plane roll-up served by GET /api/v1/admin/usage (platform-admin only).

export interface TenantUsage {
  tenant_id: string;
  tenant_name?: string;
  alerts_in_window: number;
  avg_events_per_day: number;
  eps: number;
  total_alerts_stored: number;
  active_users: number;
  active_integrations: number;
  open_incidents: number;
  // Billing plan + quota limits (B2 fatia 2), embedded per tenant. -1 limits mean unlimited. Absent
  // on the platform `totals` aggregate.
  plan_name?: string;
  max_alerts_per_month?: number;
  max_integrations?: number;
  max_users?: number;
}

export interface PlatformUsage {
  window_days: number;
  tenant_count: number;
  tenants: TenantUsage[];
  totals: TenantUsage;
}
