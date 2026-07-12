export type AlertSeverity = 'info' | 'warning' | 'critical' | 'fatal';
export type AlertStatus = 'triggered' | 'acknowledged' | 'resolved' | 'suppressed';

// Lifted as-is from the original inline `interface Alert` in page.tsx (lines 42-57).
export interface Alert {
  id: string;
  tenant_id: string;
  device_id?: string;
  event_type: string;
  severity: AlertSeverity;
  status: AlertStatus;
  summary: string;
  payload: Record<string, unknown>;
  ai_analysis?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
  acknowledged_at?: string;
  ai_diagnostic?: string;
  fingerprint?: string;
}

export interface AlertStats {
  total: number;
  fatal: number;
  critical: number;
  warning: number;
  info: number;
}
