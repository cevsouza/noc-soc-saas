package agent

import (
	"testing"
	"time"
)

func TestComputeBps(t *testing.T) {
	cases := []struct {
		prev, cur uint64
		dt        float64
		want      int64
	}{
		{0, 0, 10, 0},                   // no traffic
		{100, 100, 10, 0},               // idle
		{0, 1250, 10, 1000},             // 1250 bytes in 10s = 1000 bps
		{1000, 2000, 1, 8000},           // 1000 bytes in 1s = 8000 bps
		{5000, 1000, 10, 0},             // counter reset/backwards → 0, not a spike
		{0, 1000, 0, 0},                 // dt=0 guard
		{0, 1000, -5, 0},                // negative dt guard
		{1_000_000, 2_000_000, 10, 800000}, // 1MB in 10s
	}
	for _, c := range cases {
		if got := computeBps(c.prev, c.cur, c.dt); got != c.want {
			t.Errorf("computeBps(%d,%d,%v) = %d, want %d", c.prev, c.cur, c.dt, got, c.want)
		}
	}
}

func TestOperStatusLabel(t *testing.T) {
	if operStatusLabel("1") != "up" || operStatusLabel("2") != "down" || operStatusLabel("9") != "unknown" || operStatusLabel(" 1 ") != "up" {
		t.Errorf("operStatusLabel mapping wrong")
	}
}

func TestBuildInterfaceStats(t *testing.T) {
	// Two interfaces (index 1 and 2). Prev sample 10s ago for idx 1 with lower counters → positive rate.
	now := time.Now()
	prev := map[string]ifSample{
		"10.0.0.1|1": {in: 0, out: 0, ts: now.Add(-10 * time.Second)},
	}
	names := []WalkVar{{OID: oidIfName + ".1", Str: "Gi0/1"}, {OID: oidIfName + ".2", Str: "Gi0/2"}}
	inOct := []WalkVar{{OID: oidIfHCInOctets + ".1", Str: "1250"}, {OID: oidIfHCInOctets + ".2", Str: "9999"}}
	outOct := []WalkVar{{OID: oidIfHCOutOctets + ".1", Str: "2500"}, {OID: oidIfHCOutOctets + ".2", Str: "0"}}
	speed := []WalkVar{{OID: oidIfHighSpeed + ".1", Str: "1000"}, {OID: oidIfHighSpeed + ".2", Str: "100"}}
	oper := []WalkVar{{OID: oidIfOperStatus + ".1", Str: "1"}, {OID: oidIfOperStatus + ".2", Str: "2"}}

	stats, nextPrev := buildInterfaceStats("10.0.0.1", names, inOct, outOct, speed, oper, prev, now)
	if len(stats) != 2 {
		t.Fatalf("got %d stats, want 2", len(stats))
	}
	var gi1 *InterfaceStat
	for i := range stats {
		if stats[i].IfIndex == "1" {
			gi1 = &stats[i]
		}
	}
	if gi1 == nil {
		t.Fatalf("idx 1 missing")
	}
	// 1250 bytes in 10s = 1000 bps in; 2500 bytes = 2000 bps out; 1000 Mbps = 1e9 bps; up.
	if gi1.InBps != 1000 || gi1.OutBps != 2000 || gi1.SpeedBps != 1_000_000_000 || gi1.OperStatus != "up" || gi1.IfName != "Gi0/1" {
		t.Errorf("gi1 wrong: %+v", *gi1)
	}
	// idx 2 has no prev → 0 bps; oper down.
	for i := range stats {
		if stats[i].IfIndex == "2" && (stats[i].InBps != 0 || stats[i].OperStatus != "down") {
			t.Errorf("gi2 wrong: %+v", stats[i])
		}
	}
	if _, ok := nextPrev["10.0.0.1|2"]; !ok {
		t.Errorf("next prev should carry idx 2")
	}
}
