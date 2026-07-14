// Cross-tenant threat intel (Backlog B6). Mirrors internal/api/threat_intel.go +
// internal/threatintel. The aggregate is anonymized: it never says which tenants contributed.

export interface SharedIndicator {
  indicator_type: string; // 'ip'
  indicator_value: string;
  observation_count: number;
  tenant_count: number; // distinct opted-in tenants that saw it
  first_seen: string;
  last_seen: string;
}

export interface ThreatIntelResponse {
  opted_in: boolean;
  indicators: SharedIndicator[];
}
