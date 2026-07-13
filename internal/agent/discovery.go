package agent

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	gosnmp "github.com/gosnmp/gosnmp"
)

// Active network discovery (topology slice A). The agent sweeps a configured CIDR by attempting an
// SNMP GET of sysName/sysDescr/sysObjectID on every host. Any responder is a live, SNMP-capable
// device that we identify (vendor + type) in a single shot — no ICMP/raw sockets, no inbound rule.
// The sweep itself is behind the Scanner interface so Discover's fan-out/aggregation is unit-tested
// with canned responders (no live network).

// Standard SNMPv2-MIB system OIDs used to fingerprint a device.
const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
)

// maxDiscoveryHosts caps how many addresses one target may expand to, so a fat prefix can't turn a
// sweep into a multi-hour scan. 4096 == a /20; typical LANs are /24. Enforced here and in the API.
const maxDiscoveryHosts = 4096

// discoveryWorkers bounds sweep concurrency so a /24 finishes quickly without flooding the network.
const discoveryWorkers = 32

// DiscoveryTarget is a CIDR range to sweep (mirrors the API's AgentDiscoveryTarget; community
// decrypted). Config-only, delivered to the authenticated agent via /agent/config.
type DiscoveryTarget struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	Community string `json:"community"`
}

// DiscoveredDevice is one host that answered the SNMP probe, reported back to the SaaS.
type DiscoveredDevice struct {
	IP          string `json:"ip"`
	SysName     string `json:"sysname"`
	SysDescr    string `json:"sysdescr"`
	SysObjectID string `json:"sysobjectid"`
	Vendor      string `json:"vendor"`
	DeviceType  string `json:"device_type"`
}

// Scanner probes a single host for its SNMP identity and its physical neighbours. Probe's ok is false
// when the host doesn't answer; Neighbors returns the host's LLDP/CDP adjacencies (topology slice B),
// empty when it speaks neither.
type Scanner interface {
	Probe(target DiscoveryTarget, ip string) (DiscoveredDevice, bool)
	Neighbors(target DiscoveryTarget, ip string) []NeighborLink
}

// expandCIDR returns the host addresses inside a CIDR, excluding the network and broadcast addresses
// for IPv4 prefixes shorter than /31. Pure and unit-tested. Errors on an unparseable or too-large
// range rather than silently truncating.
func expandCIDR(cidr string) ([]string, error) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	ip4 := ipnet.IP.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("only IPv4 CIDRs are supported: %q", cidr)
	}
	ones, bits := ipnet.Mask.Size()
	hostBits := bits - ones
	if hostBits > 20 { // > 4096 addresses before trimming
		return nil, fmt.Errorf("CIDR %q too large (max /%d)", cidr, bits-12)
	}
	base := binary.BigEndian.Uint32(ip4)
	count := uint32(1) << uint(hostBits)

	var lo, hi uint32
	switch {
	case hostBits <= 1: // /31 and /32: every address is usable
		lo, hi = 0, count
	default: // skip network (0) and broadcast (count-1)
		lo, hi = 1, count-1
	}

	out := make([]string, 0, hi-lo)
	for i := lo; i < hi; i++ {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], base+i)
		out = append(out, net.IP(b[:]).String())
	}
	if len(out) > maxDiscoveryHosts {
		return nil, fmt.Errorf("CIDR %q expands to %d hosts (max %d)", cidr, len(out), maxDiscoveryHosts)
	}
	return out, nil
}

// classifyDevice infers (vendor, deviceType) from sysDescr/sysObjectID. Pure and unit-tested. The
// heuristics are deliberately conservative keyword matches; an unknown device is still recorded with
// vendor/type "unknown"/"network_device" so nothing is dropped.
func classifyDevice(sysDescr, sysObjectID string) (vendor, deviceType string) {
	d := strings.ToLower(sysDescr + " " + sysObjectID)

	switch {
	case strings.Contains(d, "fortigate"), strings.Contains(d, "fortinet"):
		return "Fortinet", "firewall"
	case strings.Contains(d, "pan-os"), strings.Contains(d, "palo alto"), strings.Contains(d, "panos"):
		return "Palo Alto", "firewall"
	case strings.Contains(d, "sophos"):
		return "Sophos", "firewall"
	case strings.Contains(d, "mikrotik"), strings.Contains(d, "routeros"):
		return "MikroTik", "router"
	case strings.Contains(d, "unifi"), strings.Contains(d, "ubiquiti"), strings.Contains(d, "ubnt"):
		return "Ubiquiti", "access_point"
	case strings.Contains(d, "aruba"), strings.Contains(d, "instant"):
		return "Aruba", "access_point"
	case strings.Contains(d, "juniper"), strings.Contains(d, "junos"):
		return "Juniper", classifyByRole(d)
	case strings.Contains(d, "cisco"), strings.Contains(d, "ios"), strings.Contains(d, "nx-os"):
		return "Cisco", classifyByRole(d)
	case strings.Contains(d, "procurve"), strings.Contains(d, "hewlett"), strings.Contains(d, "hpe"):
		return "HPE", "switch"
	case strings.Contains(d, "vmware"), strings.Contains(d, "esxi"):
		return "VMware", "hypervisor"
	case strings.Contains(d, "windows"), strings.Contains(d, "microsoft"):
		return "Microsoft", "server"
	case strings.Contains(d, "linux"), strings.Contains(d, "ubuntu"), strings.Contains(d, "debian"), strings.Contains(d, "net-snmp"):
		return "Linux", "server"
	default:
		return "unknown", "network_device"
	}
}

// classifyByRole picks switch vs router for vendors that make both, from sysDescr keywords.
func classifyByRole(d string) string {
	switch {
	case strings.Contains(d, "switch"), strings.Contains(d, "catalyst"), strings.Contains(d, "nexus"):
		return "switch"
	case strings.Contains(d, "router"), strings.Contains(d, "isr"), strings.Contains(d, "asr"):
		return "router"
	default:
		return "network_device"
	}
}

// Discover sweeps every target's CIDR, probing each host with bounded concurrency, and returns the
// deduplicated set of responders (last-writer-wins per IP across overlapping targets) plus the
// physical neighbour links walked from each responder (topology slice B). A target whose CIDR can't be
// expanded is skipped (its error is returned aggregated) rather than aborting the run.
func Discover(scanner Scanner, targets []DiscoveryTarget) ([]DiscoveredDevice, []NeighborLink, error) {
	found := map[string]DiscoveredDevice{}
	var links []NeighborLink
	var mu sync.Mutex
	var errs []string

	for _, t := range targets {
		hosts, err := expandCIDR(t.CIDR)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		jobs := make(chan string)
		var wg sync.WaitGroup
		workers := discoveryWorkers
		if workers > len(hosts) {
			workers = len(hosts)
		}
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for ip := range jobs {
					dev, ok := scanner.Probe(t, ip)
					if !ok {
						continue
					}
					// Only responders get a neighbour walk (skipping dead hosts keeps the sweep cheap).
					nbrs := scanner.Neighbors(t, ip)
					mu.Lock()
					found[dev.IP] = dev
					links = append(links, nbrs...)
					mu.Unlock()
				}
			}()
		}
		for _, ip := range hosts {
			jobs <- ip
		}
		close(jobs)
		wg.Wait()
	}

	out := make([]DiscoveredDevice, 0, len(found))
	for _, d := range found {
		out = append(out, d)
	}
	if len(errs) > 0 {
		return out, links, fmt.Errorf("discovery: %s", strings.Join(errs, "; "))
	}
	return out, links, nil
}

// snmpScanner is the real SNMP v2c probe.
type snmpScanner struct{}

// NewScanner returns the production SNMP discovery scanner.
func NewScanner() Scanner { return &snmpScanner{} }

func (s *snmpScanner) Probe(t DiscoveryTarget, ip string) (DiscoveredDevice, bool) {
	port := t.Port
	if port == 0 {
		port = 161
	}
	g := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      uint16(port),
		Community: t.Community,
		Version:   gosnmp.Version2c,
		Timeout:   1 * time.Second,
		Retries:   0,
	}
	if err := g.Connect(); err != nil {
		return DiscoveredDevice{}, false
	}
	defer g.Conn.Close()

	res, err := g.Get([]string{oidSysDescr, oidSysObjectID, oidSysName})
	if err != nil || len(res.Variables) == 0 {
		return DiscoveredDevice{}, false
	}
	dev := DiscoveredDevice{IP: ip}
	answered := false
	for _, v := range res.Variables {
		if v.Value == nil {
			continue
		}
		switch normOID(v.Name) {
		case normOID(oidSysDescr):
			dev.SysDescr = snmpString(v)
			answered = true
		case normOID(oidSysName):
			dev.SysName = snmpString(v)
			answered = true
		case normOID(oidSysObjectID):
			dev.SysObjectID = snmpOIDString(v)
			answered = true
		}
	}
	if !answered {
		return DiscoveredDevice{}, false
	}
	dev.Vendor, dev.DeviceType = classifyDevice(dev.SysDescr, dev.SysObjectID)
	return dev, true
}

// snmpString renders an OctetString/other SNMP value as text.
func snmpString(v gosnmp.SnmpPDU) string {
	if b, ok := v.Value.([]byte); ok {
		return strings.TrimSpace(string(b))
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v.Value))
}

// snmpOIDString renders an ObjectIdentifier value (gosnmp returns it as a string).
func snmpOIDString(v gosnmp.SnmpPDU) string {
	if s, ok := v.Value.(string); ok {
		return strings.TrimSpace(s)
	}
	return snmpString(v)
}
