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
  // Stale marks a discovered device the agent hasn't re-observed within the freshness window (T-B).
  stale?: boolean;
  last_seen?: string;
  // CMDB location of the asset this node maps to, when set — used to group the map by site (T-C).
  location?: string;
}

export interface GraphEdge {
  source: string;
  target: string;
  protocol: string;
  local_port?: string;
  remote_port?: string;
  // Link utilization overlay (T-D), present when the local device's interface has SNMP stats.
  ifname?: string;
  oper_status?: string;
  in_bps?: number;
  out_bps?: number;
  speed_bps?: number;
  util_pct: number; // busier direction % of speed; -1 = no interface data
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
