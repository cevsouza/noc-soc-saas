// Mirrors internal/api/agent_metrics.go — SNMP time-series graphs (slice 3).
export interface MetricCatalogEntry {
  target_id: string | null;
  oid: string;
  label: string;
}

export interface MetricSeriesEntry {
  ts: string;
  value: number;
}
