// Active network discovery (topology slice A). The agent sweeps configured CIDR ranges via SNMP and
// reports back the responders it identifies. These mirror the Go API structs.

export interface DiscoveryTarget {
  id: string;
  name: string;
  cidr: string;
  port: number;
  version: string;
}

export interface DiscoveredDevice {
  id: string;
  ip: string;
  sysname: string;
  sysdescr: string;
  sysobjectid: string;
  vendor: string;
  device_type: string;
  first_seen: string;
  last_seen: string;
}
