package agent

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	gosnmp "github.com/gosnmp/gosnmp"
)

// Physical neighbourhood discovery (topology slice B). For each device the sweep identifies, the agent
// also walks its LLDP (IEEE 802.1AB) and CDP (Cisco) neighbour tables over SNMP to learn the real
// host-to-host EDGES. The parsing of the walked columns into links is pure and unit-tested; the live
// BulkWalk is behind the same Scanner interface as the identity probe.

// LLDP-MIB remote-system columns (lldpRemTable).
const (
	oidLLDPRemChassisID = "1.0.8802.1.1.2.1.4.1.1.5"
	oidLLDPRemPortID    = "1.0.8802.1.1.2.1.4.1.1.7"
	oidLLDPRemSysName   = "1.0.8802.1.1.2.1.4.1.1.9"
)

// CDP-MIB cache columns (cdpCacheTable).
const (
	oidCDPCacheDeviceID   = "1.3.6.1.4.1.9.9.23.1.2.1.1.6"
	oidCDPCacheDevicePort = "1.3.6.1.4.1.9.9.23.1.2.1.1.7"
)

// NeighborLink is one directed adjacency: the local device (local_ip) sees a remote neighbour on a
// local port. remote_chassis_id/remote_port_id are the stable neighbour identity (LLDP); for CDP the
// device id doubles as the chassis id.
type NeighborLink struct {
	LocalIP         string `json:"local_ip"`
	LocalPort       string `json:"local_port"`
	RemoteSysName   string `json:"remote_sysname"`
	RemoteChassisID string `json:"remote_chassis_id"`
	RemotePortID    string `json:"remote_port_id"`
	Protocol        string `json:"protocol"` // "lldp" | "cdp"
}

// WalkVar is one row from an SNMP walk, coerced to a printable string (pure code operates on these).
type WalkVar struct {
	OID string
	Str string
}

// indexAfter returns the table index (the OID suffix after root), with any leading dots trimmed.
// Empty string if oid is not under root. Pure.
func indexAfter(oid, root string) string {
	o := strings.TrimPrefix(oid, ".")
	r := strings.TrimPrefix(root, ".")
	if !strings.HasPrefix(o, r+".") {
		return ""
	}
	return strings.TrimPrefix(o, r+".")
}

// byIndex maps each walked var to its table index under root.
func byIndex(vars []WalkVar, root string) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		if idx := indexAfter(v.OID, root); idx != "" {
			m[idx] = v.Str
		}
	}
	return m
}

// buildLLDPLinks joins the three LLDP columns by their shared index into neighbour links. The LLDP
// index is timeMark.localPortNum.remIndex, so the local port number is the second-to-last component.
// Pure and unit-tested.
func buildLLDPLinks(localIP string, chassis, port, sysname []WalkVar) []NeighborLink {
	chassisByIdx := byIndex(chassis, oidLLDPRemChassisID)
	portByIdx := byIndex(port, oidLLDPRemPortID)
	sysByIdx := byIndex(sysname, oidLLDPRemSysName)

	links := make([]NeighborLink, 0, len(chassisByIdx))
	for idx, ch := range chassisByIdx {
		parts := strings.Split(idx, ".")
		localPort := ""
		if len(parts) >= 3 {
			localPort = parts[len(parts)-2]
		}
		links = append(links, NeighborLink{
			LocalIP:         localIP,
			LocalPort:       localPort,
			RemoteSysName:   sysByIdx[idx],
			RemoteChassisID: ch,
			RemotePortID:    portByIdx[idx],
			Protocol:        "lldp",
		})
	}
	return links
}

// buildCDPLinks joins the CDP device-id and device-port columns by index. The CDP index is
// ifIndex.deviceIndex, so the local port (ifIndex) is the first component. CDP has no chassis id, so
// the device id doubles as the stable neighbour identity. Pure and unit-tested.
func buildCDPLinks(localIP string, deviceID, devicePort []WalkVar) []NeighborLink {
	idByIdx := byIndex(deviceID, oidCDPCacheDeviceID)
	portByIdx := byIndex(devicePort, oidCDPCacheDevicePort)

	links := make([]NeighborLink, 0, len(idByIdx))
	for idx, dev := range idByIdx {
		parts := strings.Split(idx, ".")
		localPort := ""
		if len(parts) >= 1 {
			localPort = parts[0]
		}
		links = append(links, NeighborLink{
			LocalIP:         localIP,
			LocalPort:       localPort,
			RemoteSysName:   dev,
			RemoteChassisID: dev,
			RemotePortID:    portByIdx[idx],
			Protocol:        "cdp",
		})
	}
	return links
}

// Neighbors on the real snmpScanner walks LLDP then CDP and returns the joined links. A device that
// speaks neither (or is unreachable) yields no links, never an error that aborts the sweep.
func (s *snmpScanner) Neighbors(t DiscoveryTarget, ip string) []NeighborLink {
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

	var links []NeighborLink
	links = append(links, buildLLDPLinks(ip, walk(oidLLDPRemChassisID), walk(oidLLDPRemPortID), walk(oidLLDPRemSysName))...)
	links = append(links, buildCDPLinks(ip, walk(oidCDPCacheDeviceID), walk(oidCDPCacheDevicePort))...)
	return links
}

// pduStr renders an SNMP value as a printable string: readable text as-is, raw bytes (e.g. a MAC
// chassis id) as colon-separated hex.
func pduStr(v interface{}) string {
	switch val := v.(type) {
	case []byte:
		if isPrintable(val) {
			return strings.TrimSpace(string(val))
		}
		return hexColon(val)
	case string:
		return strings.TrimSpace(val)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", val))
	}
}

func isPrintable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

func hexColon(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = hex.EncodeToString([]byte{c})
	}
	return strings.Join(parts, ":")
}
