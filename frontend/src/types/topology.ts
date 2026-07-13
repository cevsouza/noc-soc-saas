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

// Merged topology graph (discovery slice C): discovered devices + alert hosts + physical LLDP/CDP
// edges. Mirrors internal/api/topology_graph.go.
export interface GraphNode {
  id: string;
  label: string;
  kind: string; // device_type, or "host" / "neighbor"
  vendor?: string;
  origin: 'discovery' | 'telemetry' | 'both' | 'neighbor';
  worst_severity: string;
  unresolved_alerts: number;
}

export interface GraphEdge {
  source: string;
  target: string;
  protocol: string;
  local_port?: string;
  remote_port?: string;
}

export interface TopologyGraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
}
