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

// Discovery wiring status (topology slice T1). Tells the topology tab whether an agent is connected
// and how much active discovery has found, so an empty graph can explain itself. Mirrors
// internal/api/topology.go TopologyStatus.
export interface TopologyStatus {
  agent_count: number;
  agent_connected: boolean;
  last_seen: string | null;
  discovery_targets: number;
  discovered_devices: number;
  discovered_links: number;
}
