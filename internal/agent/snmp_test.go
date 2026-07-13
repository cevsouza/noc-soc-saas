package agent

import (
	"errors"
	"testing"
)

func TestEvaluateCheck(t *testing.T) {
	cases := []struct {
		val, thr float64
		cmp      string
		want     bool
	}{
		{95, 90, "gt", true},
		{50, 90, "gt", false},
		{5, 10, "lt", true},
		{10, 10, "ge", true},
		{10, 10, "le", true},
		{10, 10, "eq", true},
		{11, 10, "eq", false},
		{11, 10, "ne", true},
		{5, 10, "bogus", false},
	}
	for _, c := range cases {
		if got := evaluateCheck(c.val, c.thr, c.cmp); got != c.want {
			t.Errorf("evaluateCheck(%v,%v,%q)=%v want %v", c.val, c.thr, c.cmp, got, c.want)
		}
	}
}

func TestSNMPToFloat(t *testing.T) {
	for _, v := range []interface{}{int(3), int64(3), uint(3), uint32(3), uint64(3), float64(3)} {
		if f, ok := snmpToFloat(v); !ok || f != 3 {
			t.Errorf("snmpToFloat(%T)=%v,%v", v, f, ok)
		}
	}
	if _, ok := snmpToFloat("nope"); ok {
		t.Error("string must not coerce")
	}
}

type fakePoller struct {
	vals map[string]float64
	err  error
}

func (f fakePoller) Poll(_ SNMPTarget, _ []string) (map[string]float64, error) {
	return f.vals, f.err
}

func TestCollectEmitsOnlyBreaches(t *testing.T) {
	target := SNMPTarget{
		Name: "core-sw", Host: "10.0.0.1", Port: 161, Community: "public",
		Checks: []Check{
			{OID: ".1.3.6.1.4.1.9.9.109.1.1.1.1.5.1", Label: "cpu_high", Comparison: "gt", Threshold: 90, Severity: "critical"},
			{OID: "1.3.6.1.2.1.25.2.3.1.6.1", Label: "mem_high", Comparison: "gt", Threshold: 90, Severity: "warning"},
		},
	}
	// cpu breaches (95>90), mem does not (40<90). Note requested OID keyed without leading dot.
	poller := fakePoller{vals: map[string]float64{
		"1.3.6.1.4.1.9.9.109.1.1.1.1.5.1": 95,
		"1.3.6.1.2.1.25.2.3.1.6.1":        40,
	}}
	events := Collect(poller, []SNMPTarget{target})
	if len(events) != 1 {
		t.Fatalf("want 1 breach event, got %d: %+v", len(events), events)
	}
	e := events[0]
	if e.EventType != "cpu_high" || e.Severity != "critical" || e.Host != "10.0.0.1" || e.Source != "snmp" {
		t.Fatalf("unexpected event: %+v", e)
	}
	if e.ExternalID != "10.0.0.1:1.3.6.1.4.1.9.9.109.1.1.1.1.5.1" {
		t.Fatalf("external_id = %q", e.ExternalID)
	}
}

func TestCollectUnreachableEmitsWarning(t *testing.T) {
	target := SNMPTarget{Name: "edge", Host: "10.0.0.2", Checks: []Check{{OID: "1.2.3", Label: "x", Comparison: "gt", Threshold: 1, Severity: "critical"}}}
	events := Collect(fakePoller{err: errors.New("timeout")}, []SNMPTarget{target})
	if len(events) != 1 || events[0].EventType != "snmp_unreachable" || events[0].Severity != "warning" {
		t.Fatalf("want 1 unreachable warning, got %+v", events)
	}
}
