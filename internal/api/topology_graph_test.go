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

	g := buildTopologyGraph(devices, links, hosts, nil)

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
	g := buildTopologyGraph(devices, links, nil, nil)
	if len(g.Edges) != 1 {
		t.Errorf("got %d edges, want 1 (deduped)", len(g.Edges))
	}
}

func TestBuildTopologyGraphRobustMatching(t *testing.T) {
	devices := []GraphNodeIn{
		{IP: "192.168.1.1", SysName: "edge-fw", Vendor: "Fortinet", DeviceType: "firewall"},
		{IP: "192.168.1.2", SysName: "core-sw", Vendor: "Cisco", DeviceType: "switch"},
	}
	hosts := []GraphHostIn{
		// FQDN of edge-fw: must fold onto the device by short-name, not become a separate node.
		{Host: "EDGE-FW.corp.example.com", UnresolvedAlerts: 1, WorstSeverity: "critical"},
		// A DNS alias the monitoring tool uses for core-sw, mapped manually via the CMDB.
		{Host: "switch01.mon", UnresolvedAlerts: 2, WorstSeverity: "warning"},
	}
	aliases := []AssetAlias{
		{Identifier: "192.168.1.2", Aliases: []string{"switch01.mon", "sw-core"}},
	}

	g := buildTopologyGraph(devices, nil, hosts, aliases)

	// Both hosts collapse onto their devices → only 2 nodes, no telemetry duplicates.
	if len(g.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2 (no duplicates): %+v", len(g.Nodes), g.Nodes)
	}
	fw := findNode(g, "192.168.1.1")
	if fw == nil || fw.Origin != "both" || fw.WorstSeverity != "critical" {
		t.Errorf("edge-fw should merge via FQDN short-name: %+v", fw)
	}
	sw := findNode(g, "192.168.1.2")
	if sw == nil || sw.Origin != "both" || sw.UnresolvedAlerts != 2 {
		t.Errorf("core-sw should merge via CMDB alias: %+v", sw)
	}
}

func TestBuildTopologyGraphAggregatesHosts(t *testing.T) {
	// Two alert-hosts (FQDN + CMDB alias) both map to the same switch: the node must aggregate the open
	// counts (1+1) and take the worst severity (critical), not overwrite with whichever folds last.
	devices := []GraphNodeIn{{IP: "192.168.70.2", SysName: "core-sw", DeviceType: "switch"}}
	hosts := []GraphHostIn{
		{Host: "core-sw.corp.example.com", UnresolvedAlerts: 1, WorstSeverity: "critical"},
		{Host: "switch01.mon", UnresolvedAlerts: 1, WorstSeverity: "warning"},
	}
	aliases := []AssetAlias{{Identifier: "192.168.70.2", Aliases: []string{"switch01.mon"}}}

	g := buildTopologyGraph(devices, nil, hosts, aliases)
	if len(g.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 (both hosts folded): %+v", len(g.Nodes), g.Nodes)
	}
	n := g.Nodes[0]
	if n.Origin != "both" || n.UnresolvedAlerts != 2 || n.WorstSeverity != "critical" {
		t.Errorf("aggregation wrong: origin=%s unresolved=%d worst=%s", n.Origin, n.UnresolvedAlerts, n.WorstSeverity)
	}
}

func TestBuildTopologyGraphAmbiguousShortName(t *testing.T) {
	// Two devices share the short-name "fw" (different domains). An ambiguous short-name must NOT be
	// guessed — the host stays a separate telemetry node rather than merging onto the wrong device.
	devices := []GraphNodeIn{
		{IP: "10.0.0.1", SysName: "fw.site-a"},
		{IP: "10.0.0.2", SysName: "fw.site-b"},
	}
	hosts := []GraphHostIn{{Host: "fw", UnresolvedAlerts: 1, WorstSeverity: "warning"}}

	g := buildTopologyGraph(devices, nil, hosts, nil)
	if len(g.Nodes) != 3 {
		t.Fatalf("got %d nodes, want 3 (ambiguous short-name not guessed): %+v", len(g.Nodes), g.Nodes)
	}
}
