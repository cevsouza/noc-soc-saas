package agent

import (
	"sort"
	"testing"
)

func TestIndexAfter(t *testing.T) {
	if got := indexAfter("1.0.8802.1.1.2.1.4.1.1.5.0.3.1", oidLLDPRemChassisID); got != "0.3.1" {
		t.Errorf("indexAfter = %q, want 0.3.1", got)
	}
	if got := indexAfter(".1.0.8802.1.1.2.1.4.1.1.5.0.3.1", oidLLDPRemChassisID); got != "0.3.1" {
		t.Errorf("indexAfter (leading dot) = %q, want 0.3.1", got)
	}
	if got := indexAfter("1.2.3.4", oidLLDPRemChassisID); got != "" {
		t.Errorf("indexAfter unrelated = %q, want empty", got)
	}
}

func TestBuildLLDPLinks(t *testing.T) {
	// index = timeMark.localPortNum.remIndex; local port is the middle component (here "3").
	root := oidLLDPRemChassisID
	chassis := []WalkVar{{OID: root + ".0.3.1", Str: "aa:bb:cc:dd:ee:ff"}}
	port := []WalkVar{{OID: oidLLDPRemPortID + ".0.3.1", Str: "Gi0/1"}}
	sysname := []WalkVar{{OID: oidLLDPRemSysName + ".0.3.1", Str: "core-sw"}}

	links := buildLLDPLinks("10.0.0.2", chassis, port, sysname)
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1", len(links))
	}
	l := links[0]
	if l.LocalIP != "10.0.0.2" || l.LocalPort != "3" || l.RemoteSysName != "core-sw" ||
		l.RemoteChassisID != "aa:bb:cc:dd:ee:ff" || l.RemotePortID != "Gi0/1" || l.Protocol != "lldp" {
		t.Errorf("link = %+v", l)
	}
}

func TestBuildCDPLinks(t *testing.T) {
	// index = ifIndex.deviceIndex; local port is the first component (here "10").
	devID := []WalkVar{{OID: oidCDPCacheDeviceID + ".10.1", Str: "neighbor-router"}}
	devPort := []WalkVar{{OID: oidCDPCacheDevicePort + ".10.1", Str: "FastEthernet0/2"}}

	links := buildCDPLinks("10.0.0.5", devID, devPort)
	if len(links) != 1 {
		t.Fatalf("got %d links, want 1", len(links))
	}
	l := links[0]
	if l.LocalPort != "10" || l.RemoteSysName != "neighbor-router" || l.RemoteChassisID != "neighbor-router" ||
		l.RemotePortID != "FastEthernet0/2" || l.Protocol != "cdp" {
		t.Errorf("link = %+v", l)
	}
}

func TestBuildLLDPLinksMultiple(t *testing.T) {
	root := oidLLDPRemChassisID
	chassis := []WalkVar{
		{OID: root + ".0.1.1", Str: "mac-a"},
		{OID: root + ".0.2.1", Str: "mac-b"},
	}
	port := []WalkVar{
		{OID: oidLLDPRemPortID + ".0.1.1", Str: "p1"},
		{OID: oidLLDPRemPortID + ".0.2.1", Str: "p2"},
	}
	sysname := []WalkVar{
		{OID: oidLLDPRemSysName + ".0.1.1", Str: "peer1"},
		{OID: oidLLDPRemSysName + ".0.2.1", Str: "peer2"},
	}
	links := buildLLDPLinks("10.0.0.9", chassis, port, sysname)
	if len(links) != 2 {
		t.Fatalf("got %d links, want 2", len(links))
	}
	sort.Slice(links, func(i, j int) bool { return links[i].RemoteSysName < links[j].RemoteSysName })
	if links[0].LocalPort != "1" || links[0].RemoteSysName != "peer1" {
		t.Errorf("link0 = %+v", links[0])
	}
	if links[1].LocalPort != "2" || links[1].RemoteSysName != "peer2" {
		t.Errorf("link1 = %+v", links[1])
	}
}

func TestPduStr(t *testing.T) {
	if got := pduStr([]byte("core-sw")); got != "core-sw" {
		t.Errorf("printable bytes = %q", got)
	}
	if got := pduStr([]byte{0xaa, 0xbb}); got != "aa:bb" {
		t.Errorf("raw bytes = %q, want aa:bb", got)
	}
	if got := pduStr("  spaced  "); got != "spaced" {
		t.Errorf("string trim = %q", got)
	}
}
