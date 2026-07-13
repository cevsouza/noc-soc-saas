package agent

import (
	"strings"
	"time"

	gosnmp "github.com/gosnmp/gosnmp"
)

// ARP discovery (topology slice T5). For each SNMP device the sweep identifies, the agent also walks its
// ARP cache (ipNetToMedia) to learn IP↔MAC entries — that's how it finds hosts that DON'T speak SNMP
// (endpoints, printers, IoT). The parsing of the walked table into hosts is pure and unit-tested; the
// live BulkWalk is behind the same Scanner interface as the identity probe.

// IP-MIB ipNetToMediaTable physical-address column. Each row is indexed by <ifIndex>.<a>.<b>.<c>.<d>
// (the neighbour IPv4 address), and the value is the neighbour's MAC.
const oidIPNetToMediaPhysAddress = "1.3.6.1.2.1.4.22.1.2"

// ARPHost is one IP↔MAC entry learned from a device's ARP cache — a host reachable on the network that
// may or may not speak SNMP.
type ARPHost struct {
	IP  string `json:"ip"`
	MAC string `json:"mac"`
}

// parseARPEntries turns a walk of ipNetToMediaPhysAddress into IP↔MAC hosts. The neighbour IP is the
// last four components of the table index; the MAC is the walked value. Entries with a malformed index
// or an all-zero MAC are dropped. Pure and unit-tested.
func parseARPEntries(vars []WalkVar) []ARPHost {
	out := make([]ARPHost, 0, len(vars))
	seen := map[string]bool{}
	for _, v := range vars {
		idx := indexAfter(v.OID, oidIPNetToMediaPhysAddress)
		if idx == "" {
			continue
		}
		parts := strings.Split(idx, ".")
		if len(parts) < 4 {
			continue
		}
		ip := strings.Join(parts[len(parts)-4:], ".")
		mac := strings.TrimSpace(v.Str)
		if ip == "" || mac == "" || isZeroMAC(mac) || seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ARPHost{IP: ip, MAC: mac})
	}
	return out
}

// isZeroMAC reports whether a colon-hex MAC is all zeros (an incomplete/placeholder ARP entry).
func isZeroMAC(mac string) bool {
	for _, p := range strings.Split(mac, ":") {
		if strings.Trim(p, "0") != "" {
			return false
		}
	}
	return true
}

// ARPTable on the real snmpScanner walks the device's ipNetToMedia table and returns its ARP hosts. A
// device that doesn't expose it (or is unreachable) yields no hosts, never an error that aborts the sweep.
func (s *snmpScanner) ARPTable(t DiscoveryTarget, ip string) []ARPHost {
	port := t.Port
	if port == 0 {
		port = 161
	}
	g := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      uint16(port),
		Community: t.Community,
		Version:   gosnmp.Version2c,
		Timeout:   2 * time.Second,
		Retries:   0,
		MaxOids:   gosnmp.MaxOids,
	}
	if err := g.Connect(); err != nil {
		return nil
	}
	defer g.Conn.Close()

	pdus, err := g.BulkWalkAll(oidIPNetToMediaPhysAddress)
	if err != nil {
		return nil
	}
	vars := make([]WalkVar, 0, len(pdus))
	for _, p := range pdus {
		vars = append(vars, WalkVar{OID: p.Name, Str: pduStr(p.Value)})
	}
	return parseARPEntries(vars)
}
