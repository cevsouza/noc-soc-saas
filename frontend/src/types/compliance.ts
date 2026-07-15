// Mirrors internal/api/compliance_report.go — the tenant's governance posture (B4).
export interface ComplianceReport {
  generated_at: string;
  alerts_retention_enabled: boolean;
  alerts_retention_days: number;
  total_alerts: number;
  oldest_alert: string | null;
  total_incidents: number;
  audit_entries: number;
  oldest_audit: string | null;
  audit_append_only: boolean;
  tenant_isolation_rls: boolean;
  per_tenant_encryption: boolean;
  suppression_rules: number;
  sla_customized: boolean;
}
