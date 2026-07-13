// Mirrors internal/api/topology.go — the tenant's real asset map, derived from the hosts present
// in its alert stream (not a hardcoded diagram). Each node is a host that has reported telemetry
// in the 30-day window.

export interface TopologyNode {
  host: string;
  total_alerts: number;
  unresolved_alerts: number;
  worst_severity: string; // "" when nothing is currently unresolved
  sources: string[];
  last_seen: string;
}

export interface TopologyResponse {
  window_days: number;
  total_assets: number;
  nodes: TopologyNode[];
}
