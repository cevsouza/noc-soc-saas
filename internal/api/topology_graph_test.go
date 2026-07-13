package api

import "testing"

func findNode(g TopologyGraph, id string) *GraphNode {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

func TestBuildTopologyGraph(t *testing.T) {
	devices := []GraphNodeIn{
		{IP: "192.168.1.1", SysName: "core-sw", Vendor: "Cisco", DeviceType: "switch"},
		{IP: "192.168.1.2", SysName: "edge-fw", Vendor: "Fortinet", DeviceType: "firewall"},
	}
	links := []GraphLinkIn{
		// core-sw -> edge-fw, resolvable by remote sysName to an existing device node.
		{LocalIP: "192.168.1.1", LocalPort: "3", RemoteSysName: "edge-fw", RemoteChassisID: "aa:bb", RemotePortID: "Gi0/1", Protocol: "lldp"},
		// core-sw -> an unknown neighbour (no matching device) => neighbour node.
		{LocalIP: "192.168.1.1", LocalPort: "5", RemoteSysName: "phone-x", RemoteChassisID: "cc:dd", RemotePortID: "p1", Protocol: "cdp"},
	}
	hosts := []GraphHostIn{
		// Matches the firewall by sysName => origin "both" + severity.
		{Host: "edge-fw", UnresolvedAlerts: 2, WorstSeverity: "critical"},
		// No matching device => telemetry-only node.
		{Host: "app-prod-01", UnresolvedAlerts: 1, WorstSeverity: "warning"},
	}

	g := buildTopologyGraph(devices, links, hosts)

	// Nodes: 2 devices + 1 telemetry host + 1 neighbour = 4.
	if len(g.Nodes) != 4 {
		t.Fatalf("got %d nodes, want 4: %+v", len(g.Nodes), g.Nodes)
	}

	fw := findNode(g, "192.168.1.2")
	if fw == nil || fw.Origin != "both" || fw.WorstSeverity != "critical" || fw.UnresolvedAlerts != 2 {
		t.Errorf("firewall node should be merged 'both' with severity: %+v", fw)
	}
	sw := findNode(g, "192.168.1.1")
	if sw == nil || sw.Origin != "discovery" {
		t.Errorf("switch node should be discovery-only: %+v", sw)
	}
	host := findNode(g, "app-prod-01")
	if host == nil || host.Origin != "telemetry" || host.Kind != "host" {
		t.Errorf("telemetry host node wrong: %+v", host)
	}
	nbr := findNode(g, "nbr:cc:dd")
	if nbr == nil || nbr.Origin != "neighbor" || nbr.Label != "phone-x" {
		t.Errorf("neighbour node wrong: %+v", nbr)
	}

	// Edges: switch->firewall (resolved to device) and switch->neighbour.
	if len(g.Edges) != 2 {
		t.Fatalf("got %d edges, want 2: %+v", len(g.Edges), g.Edges)
	}
	var toFW, toNbr bool
	for _, e := range g.Edges {
		if e.Source == "192.168.1.1" && e.Target == "192.168.1.2" && e.Protocol == "lldp" && e.RemotePort == "Gi0/1" {
			toFW = true
		}
		if e.Source == "192.168.1.1" && e.Target == "nbr:cc:dd" && e.Protocol == "cdp" {
			toNbr = true
		}
	}
	if !toFW || !toNbr {
		t.Errorf("edges wrong: toFW=%v toNbr=%v (%+v)", toFW, toNbr, g.Edges)
	}
}

func TestBuildTopologyGraphDedupEdges(t *testing.T) {
	devices := []GraphNodeIn{{IP: "10.0.0.1", SysName: "a"}, {IP: "10.0.0.2", SysName: "b"}}
	// Same adjacency reported twice (e.g. both directions of the same protocol) collapses to one edge.
	links := []GraphLinkIn{
		{LocalIP: "10.0.0.1", RemoteSysName: "b", RemoteChassisID: "x", Protocol: "lldp"},
		{LocalIP: "10.0.0.1", RemoteSysName: "b", RemoteChassisID: "x", Protocol: "lldp"},
	}
	g := buildTopologyGraph(devices, links, nil)
	if len(g.Edges) != 1 {
		t.Errorf("got %d edges, want 1 (deduped)", len(g.Edges))
	}
}
