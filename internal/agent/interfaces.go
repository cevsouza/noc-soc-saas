package agent

import (
	"strconv"
	"strings"
	"time"

	gosnmp "github.com/gosnmp/gosnmp"
)

// Interface utilization collection (topology slice T-D). For each SNMP-monitored device the agent
// walks ifXTable/ifTable and computes per-interface throughput from the 64-bit HC octet counters
// (rate = delta_octets*8 / delta_seconds between cycles). The result is pushed to /agent/interfaces
// and joined to the LLDP/CDP edges by (device_ip, ifindex) so the topology graph can color links by
// real load. The counter math and column join are pure and unit-tested; the live BulkWalk mirrors the
// neighbour walk.

// ifXTable (high-capacity) and ifTable columns.
const (
	oidIfName        = "1.3.6.1.2.1.31.1.1.1.1"  // ifName
	oidIfHCInOctets  = "1.3.6.1.2.1.31.1.1.1.6"  // ifHCInOctets (64-bit)
	oidIfHCOutOctets = "1.3.6.1.2.1.31.1.1.1.10" // ifHCOutOctets (64-bit)
	oidIfHighSpeed   = "1.3.6.1.2.1.31.1.1.1.15" // ifHighSpeed (Mbps)
	oidIfOperStatus  = "1.3.6.1.2.1.2.2.1.8"     // ifOperStatus
)

// InterfaceStat is one interface's latest utilization snapshot for a device.
type InterfaceStat struct {
	DeviceIP   string `json:"device_ip"`
	IfIndex    string `json:"ifindex"`
	IfName     string `json:"ifname"`
	OperStatus string `json:"oper_status"`
	InBps      int64  `json:"in_bps"`
	OutBps     int64  `json:"out_bps"`
	SpeedBps   int64  `json:"speed_bps"`
}

// ifSample is the previous cycle's counter reading for one interface, kept to compute a rate.
type ifSample struct {
	in  uint64
	out uint64
	ts  time.Time
}

// InterfaceWalker walks a target's interface columns. Behind an interface so the collector is testable
// with canned rows (no live device), same as the neighbour Scanner.
type InterfaceWalker interface {
	// WalkInterfaces returns the ifName, ifHCInOctets, ifHCOutOctets, ifHighSpeed, ifOperStatus columns.
	WalkInterfaces(t SNMPTarget) (names, inOct, outOct, speed, oper []WalkVar)
}

// parseUint parses a decimal counter/gauge string, 0 on failure.
func parseUint(s string) uint64 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// computeBps returns the bit-rate between two 64-bit octet counters over dt seconds. A non-positive
// dt or a counter that went backwards (reset/reboot) yields 0 rather than a bogus spike. Pure.
func computeBps(prev, cur uint64, dtSeconds float64) int64 {
	if dtSeconds <= 0 || cur < prev {
		return 0
	}
	return int64(float64(cur-prev) * 8 / dtSeconds)
}

// operStatusLabel maps ifOperStatus (1=up,2=down,3=testing,…) to a short label. Pure.
func operStatusLabel(s string) string {
	switch strings.TrimSpace(s) {
	case "1":
		return "up"
	case "2":
		return "down"
	case "3":
		return "testing"
	case "5":
		return "dormant"
	case "6":
		return "notPresent"
	default:
		return "unknown"
	}
}

// buildInterfaceStats joins the walked interface columns by their shared ifIndex and computes bps vs
// the previous cycle's counters. Returns the stats and the new prev map (keyed device_ip|ifindex).
// Interfaces without a name are skipped. Pure and unit-tested.
func buildInterfaceStats(deviceIP string, names, inOct, outOct, speed, oper []WalkVar, prev map[string]ifSample, now time.Time) ([]InterfaceStat, map[string]ifSample) {
	nameByIdx := byIndex(names, oidIfName)
	inByIdx := byIndex(inOct, oidIfHCInOctets)
	outByIdx := byIndex(outOct, oidIfHCOutOctets)
	speedByIdx := byIndex(speed, oidIfHighSpeed)
	operByIdx := byIndex(oper, oidIfOperStatus)

	stats := make([]InterfaceStat, 0, len(nameByIdx))
	next := make(map[string]ifSample, len(nameByIdx))
	for idx, name := range nameByIdx {
		if strings.TrimSpace(name) == "" {
			continue
		}
		inV := parseUint(inByIdx[idx])
		outV := parseUint(outByIdx[idx])
		key := deviceIP + "|" + idx
		next[key] = ifSample{in: inV, out: outV, ts: now}

		var inBps, outBps int64
		if p, ok := prev[key]; ok && now.After(p.ts) {
			dt := now.Sub(p.ts).Seconds()
			inBps = computeBps(p.in, inV, dt)
			outBps = computeBps(p.out, outV, dt)
		}
		stats = append(stats, InterfaceStat{
			DeviceIP:   deviceIP,
			IfIndex:    idx,
			IfName:     strings.TrimSpace(name),
			OperStatus: operStatusLabel(operByIdx[idx]),
			InBps:      inBps,
			OutBps:     outBps,
			SpeedBps:   int64(parseUint(speedByIdx[idx])) * 1_000_000, // ifHighSpeed is in Mbps
		})
	}
	return stats, next
}

// InterfaceCollector holds the previous cycle's counters so the rate can be computed across polls.
type InterfaceCollector struct {
	prev map[string]ifSample
}

// NewInterfaceCollector returns a collector with empty history (first cycle reports 0 bps).
func NewInterfaceCollector() *InterfaceCollector {
	return &InterfaceCollector{prev: map[string]ifSample{}}
}

// Collect walks every target's interfaces and returns their utilization, updating the internal history.
func (ic *InterfaceCollector) Collect(walker InterfaceWalker, targets []SNMPTarget, now time.Time) []InterfaceStat {
	all := []InterfaceStat{}
	next := map[string]ifSample{}
	for _, t := range targets {
		names, inOct, outOct, speed, oper := walker.WalkInterfaces(t)
		stats, np := buildInterfaceStats(t.Host, names, inOct, outOct, speed, oper, ic.prev, now)
		all = append(all, stats...)
		for k, v := range np {
			next[k] = v
		}
	}
	ic.prev = next
	return all
}

// gosnmpInterfaceWalker is the real SNMP v2c walker.
type gosnmpInterfaceWalker struct{}

// NewInterfaceWalker returns the production interface walker.
func NewInterfaceWalker() InterfaceWalker { return &gosnmpInterfaceWalker{} }

func (gosnmpInterfaceWalker) WalkInterfaces(t SNMPTarget) (names, inOct, outOct, speed, oper []WalkVar) {
	port := t.Port
	if port == 0 {
		port = 161
	}
	g := &gosnmp.GoSNMP{
		Target:    t.Host,
		Port:      uint16(port),
		Community: t.Community,
		Version:   gosnmp.Version2c,
		Timeout:   3 * time.Second,
		Retries:   0,
		MaxOids:   gosnmp.MaxOids,
	}
	if err := g.Connect(); err != nil {
		return nil, nil, nil, nil, nil
	}
	defer g.Conn.Close()

	walk := func(root string) []WalkVar {
		pdus, err := g.BulkWalkAll(root)
		if err != nil {
			return nil
		}
		out := make([]WalkVar, 0, len(pdus))
		for _, p := range pdus {
			out = append(out, WalkVar{OID: p.Name, Str: pduStr(p.Value)})
		}
		return out
	}
	return walk(oidIfName), walk(oidIfHCInOctets), walk(oidIfHCOutOctets), walk(oidIfHighSpeed), walk(oidIfOperStatus)
}
