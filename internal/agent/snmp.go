package agent

import (
	"fmt"
	"strings"
	"time"

	gosnmp "github.com/gosnmp/gosnmp"
)

// SNMP collection (slice 2): the agent polls the OIDs of each configured target and, on a threshold
// breach, produces an alert Event pushed via /agent/events. The polling itself is behind the Poller
// interface so the evaluate-and-emit logic is unit-tested with canned values (no live device).

// Check is one threshold rule on a polled OID (mirrors the API's SNMPCheck).
type Check struct {
	OID        string  `json:"oid"`
	Label      string  `json:"label"`
	Comparison string  `json:"comparison"`
	Threshold  float64 `json:"threshold"`
	Severity   string  `json:"severity"`
}

// SNMPTarget is a device to poll (mirrors the API's AgentSNMPTarget; community decrypted).
type SNMPTarget struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Host      string  `json:"host"`
	Port      int     `json:"port"`
	Version   string  `json:"version"`
	Community string  `json:"community"`
	Checks    []Check `json:"checks"`
}

// Poller fetches the given OIDs from a target, returning normalized OID -> numeric value.
type Poller interface {
	Poll(target SNMPTarget, oids []string) (map[string]float64, error)
}

// Sample is one raw polled value, reported as a time-series metric (slice 3).
type Sample struct {
	TargetID string  `json:"target_id"`
	OID      string  `json:"oid"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
}

// normOID drops a leading dot so requested and returned OIDs compare consistently.
func normOID(oid string) string { return strings.TrimPrefix(oid, ".") }

// evaluateCheck reports whether a polled value breaches the check (pure, unit-tested).
func evaluateCheck(value, threshold float64, comparison string) bool {
	switch comparison {
	case "gt":
		return value > threshold
	case "lt":
		return value < threshold
	case "ge":
		return value >= threshold
	case "le":
		return value <= threshold
	case "eq":
		return value == threshold
	case "ne":
		return value != threshold
	default:
		return false
	}
}

// Collect polls every target and returns (a) the alert events for threshold breaches and (b) the raw
// value samples for every check (time-series metrics). A target that can't be reached yields a single
// "snmp_unreachable" warning event and no samples.
func Collect(poller Poller, targets []SNMPTarget) ([]Event, []Sample) {
	var events []Event
	var samples []Sample
	for _, t := range targets {
		oids := make([]string, 0, len(t.Checks))
		seen := map[string]bool{}
		for _, c := range t.Checks {
			n := normOID(c.OID)
			if !seen[n] {
				seen[n] = true
				oids = append(oids, c.OID)
			}
		}
		polled, err := poller.Poll(t, oids)
		if err != nil {
			events = append(events, Event{
				Source: "snmp", ExternalID: t.Host + ":unreachable", EventType: "snmp_unreachable",
				Severity: "warning", Host: t.Host,
				Title:       fmt.Sprintf("Dispositivo SNMP '%s' (%s) inacessivel", t.Name, t.Host),
				Description: err.Error(),
			})
			continue
		}
		for _, c := range t.Checks {
			v, ok := polled[normOID(c.OID)]
			if !ok {
				continue // OID not returned by the device
			}
			samples = append(samples, Sample{TargetID: t.ID, OID: normOID(c.OID), Label: c.Label, Value: v})
			if evaluateCheck(v, c.Threshold, c.Comparison) {
				events = append(events, Event{
					Source: "snmp", ExternalID: t.Host + ":" + normOID(c.OID), EventType: c.Label,
					Severity: c.Severity, Host: t.Host,
					Title: fmt.Sprintf("%s em %s: %.2f %s %.2f", c.Label, t.Host, v, c.Comparison, c.Threshold),
				})
			}
		}
	}
	return events, samples
}

// gosnmpPoller is the real SNMP v2c poller.
type gosnmpPoller struct{}

// NewPoller returns the production SNMP poller.
func NewPoller() Poller { return &gosnmpPoller{} }

func (p *gosnmpPoller) Poll(t SNMPTarget, oids []string) (map[string]float64, error) {
	port := t.Port
	if port == 0 {
		port = 161
	}
	g := &gosnmp.GoSNMP{
		Target:    t.Host,
		Port:      uint16(port),
		Community: t.Community,
		Version:   gosnmp.Version2c,
		Timeout:   5 * time.Second,
		Retries:   1,
	}
	if err := g.Connect(); err != nil {
		return nil, err
	}
	defer g.Conn.Close()

	result, err := g.Get(oids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(result.Variables))
	for _, v := range result.Variables {
		if f, ok := snmpToFloat(v.Value); ok {
			out[normOID(v.Name)] = f
		}
	}
	return out, nil
}

// snmpToFloat coerces the numeric SNMP value types to float64 (pure, unit-tested).
func snmpToFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
