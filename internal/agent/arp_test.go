package agent

import "testing"

func TestParseARPEntries(t *testing.T) {
	root := oidIPNetToMediaPhysAddress
	vars := []WalkVar{
		// ifIndex 2, neighbour 192.168.1.50 -> a real MAC.
		{OID: root + ".2.192.168.1.50", Str: "aa:bb:cc:00:11:22"},
		// ifIndex 3, neighbour 10.0.0.7 -> a real MAC.
		{OID: root + ".3.10.0.0.7", Str: "de:ad:be:ef:00:07"},
		// Incomplete entry (all-zero MAC) must be dropped.
		{OID: root + ".2.192.168.1.99", Str: "00:00:00:00:00:00"},
		// Duplicate IP (learned on two interfaces) keeps only the first.
		{OID: root + ".9.192.168.1.50", Str: "aa:bb:cc:00:11:22"},
		// Not under the ARP root -> ignored.
		{OID: "1.3.6.1.2.1.1.5.0", Str: "some-sysname"},
	}

	hosts := parseARPEntries(vars)
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts, want 2: %+v", len(hosts), hosts)
	}
	byIP := map[string]string{}
	for _, h := range hosts {
		byIP[h.IP] = h.MAC
	}
	if byIP["192.168.1.50"] != "aa:bb:cc:00:11:22" {
		t.Errorf("192.168.1.50 mac = %q", byIP["192.168.1.50"])
	}
	if byIP["10.0.0.7"] != "de:ad:be:ef:00:07" {
		t.Errorf("10.0.0.7 mac = %q", byIP["10.0.0.7"])
	}
	if _, ok := byIP["192.168.1.99"]; ok {
		t.Error("all-zero MAC entry should have been dropped")
	}
}
