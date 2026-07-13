package agent

import (
	"sort"
	"testing"
)

func TestExpandCIDR(t *testing.T) {
	// /30 => 4 addresses, minus network + broadcast = 2 usable hosts.
	hosts, err := expandCIDR("192.168.1.0/30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"192.168.1.1", "192.168.1.2"}
	if len(hosts) != len(want) {
		t.Fatalf("got %d hosts, want %d (%v)", len(hosts), len(want), hosts)
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Errorf("host[%d]=%s want %s", i, hosts[i], want[i])
		}
	}

	// /32 => the single address is usable (no network/broadcast trim).
	single, err := expandCIDR("10.0.0.5/32")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(single) != 1 || single[0] != "10.0.0.5" {
		t.Errorf("/32 got %v, want [10.0.0.5]", single)
	}

	// /24 => 254 usable hosts.
	c24, err := expandCIDR("172.16.5.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c24) != 254 {
		t.Errorf("/24 got %d hosts, want 254", len(c24))
	}

	// Too large => error, not truncation.
	if _, err := expandCIDR("10.0.0.0/8"); err == nil {
		t.Error("expected error for oversized CIDR, got nil")
	}
	// Garbage and IPv6 => error.
	if _, err := expandCIDR("not-a-cidr"); err == nil {
		t.Error("expected error for invalid CIDR")
	}
	if _, err := expandCIDR("2001:db8::/64"); err == nil {
		t.Error("expected error for IPv6 CIDR")
	}
}

func TestClassifyDevice(t *testing.T) {
	cases := []struct {
		descr, oid       string
		vendor, devType  string
	}{
		{"FortiGate-60F v7.2.1", "", "Fortinet", "firewall"},
		{"Palo Alto Networks PAN-OS 10.1", "", "Palo Alto", "firewall"},
		{"RouterOS RB750", "", "MikroTik", "router"},
		{"UniFi AP-AC-Pro", "", "Ubiquiti", "access_point"},
		{"Cisco IOS Software, Catalyst 2960", "", "Cisco", "switch"},
		{"Cisco IOS Software, ISR 4331 Router", "", "Cisco", "router"},
		{"Linux fw01 5.15.0 net-snmp", "", "Linux", "server"},
		{"Some Unknown Device 1.0", "1.3.6.1.4.1.99999", "unknown", "network_device"},
	}
	for _, c := range cases {
		v, d := classifyDevice(c.descr, c.oid)
		if v != c.vendor || d != c.devType {
			t.Errorf("classify(%q) = (%s,%s), want (%s,%s)", c.descr, v, d, c.vendor, c.devType)
		}
	}
}

// fakeScanner answers only for the IPs in its map, and reports a canned neighbour per responder.
type fakeScanner struct {
	live  map[string]DiscoveredDevice
	nbrs  map[string][]NeighborLink
}

func (f fakeScanner) Probe(_ DiscoveryTarget, ip string) (DiscoveredDevice, bool) {
	if d, ok := f.live[ip]; ok {
		d.IP = ip
		return d, true
	}
	return DiscoveredDevice{}, false
}

func (f fakeScanner) Neighbors(_ DiscoveryTarget, ip string) []NeighborLink {
	return f.nbrs[ip]
}

func TestDiscover(t *testing.T) {
	scanner := fakeScanner{live: map[string]DiscoveredDevice{
		"192.168.1.1": {SysName: "gw", Vendor: "MikroTik", DeviceType: "router"},
		"192.168.1.2": {SysName: "sw", Vendor: "Cisco", DeviceType: "switch"},
	}, nbrs: map[string][]NeighborLink{
		"192.168.1.2": {{LocalIP: "192.168.1.2", LocalPort: "1", RemoteSysName: "gw", Protocol: "lldp"}},
	}}
	devices, links, err := Discover(scanner, []DiscoveryTarget{{CIDR: "192.168.1.0/29"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}
	if len(links) != 1 || links[0].RemoteSysName != "gw" {
		t.Errorf("got links %+v, want 1 link to gw", links)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].IP < devices[j].IP })
	if devices[0].IP != "192.168.1.1" || devices[0].Vendor != "MikroTik" {
		t.Errorf("device[0] = %+v", devices[0])
	}
	if devices[1].IP != "192.168.1.2" || devices[1].DeviceType != "switch" {
		t.Errorf("device[1] = %+v", devices[1])
	}

	// A bad CIDR is skipped with an aggregated error, but good targets still return.
	devices, _, err = Discover(scanner, []DiscoveryTarget{{CIDR: "bad"}, {CIDR: "192.168.1.0/29"}})
	if err == nil {
		t.Error("expected aggregated error for bad CIDR")
	}
	if len(devices) != 2 {
		t.Errorf("good target should still yield 2 devices, got %d", len(devices))
	}
}
