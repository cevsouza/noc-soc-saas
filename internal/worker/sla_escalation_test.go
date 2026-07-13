package worker

import "testing"

func TestEvaluateSLABreach(t *testing.T) {
	// targets: mtta=10, mttr=30 (like critical defaults)
	const mtta, mttr = 10.0, 30.0

	cases := []struct {
		name         string
		age          float64
		acknowledged bool
		resolved     bool
		want         slaBreach
	}{
		{"fresh open, within both", 5, false, false, breachNone},
		{"unacked past mtta, within mttr", 15, false, false, breachMTTA},
		{"acked past mtta, within mttr", 15, true, false, breachNone},   // ack stops the MTTA clock
		{"unacked past mttr", 45, false, false, breachMTTR},             // MTTR dominates MTTA
		{"acked but past mttr", 45, true, false, breachMTTR},            // still unresolved → page
		{"resolved never breaches", 999, false, true, breachNone},       // guard
		{"exactly at mtta target (not past)", 10, false, false, breachNone},
		{"exactly at mttr target (not past)", 30, true, false, breachNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evaluateSLABreach(c.age, c.acknowledged, c.resolved, mtta, mttr)
			if got != c.want {
				t.Fatalf("evaluateSLABreach(age=%.0f ack=%v res=%v) = %v, want %v",
					c.age, c.acknowledged, c.resolved, got.label(), c.want.label())
			}
		})
	}
}
