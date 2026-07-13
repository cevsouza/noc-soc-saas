package worker

import "testing"

// Pure-logic coverage of the watchdog decision core — no Redis/Postgres. Thresholds mirror the
// defaults: silence=600s, grace=900s.
func TestEvaluateSource(t *testing.T) {
	const (
		silence = int64(600)
		grace   = int64(900)
		now     = int64(1_000_000)
	)

	cases := []struct {
		name         string
		lastSeen     int64
		hasHeartbeat bool
		createdAt    int64
		alarmed      bool
		want         watchdogDecision
	}{
		{
			name:         "fresh heartbeat, not alarmed -> none",
			lastSeen:     now - 60,
			hasHeartbeat: true,
			createdAt:    now - 100000,
			alarmed:      false,
			want:         decisionNone,
		},
		{
			name:         "stale heartbeat past threshold, not alarmed -> alarm",
			lastSeen:     now - (silence + 60),
			hasHeartbeat: true,
			createdAt:    now - 100000,
			alarmed:      false,
			want:         decisionAlarm,
		},
		{
			name:         "stale heartbeat, already alarmed -> none (suppressed)",
			lastSeen:     now - (silence + 60),
			hasHeartbeat: true,
			createdAt:    now - 100000,
			alarmed:      true,
			want:         decisionNone,
		},
		{
			name:         "recovered: fresh heartbeat but still flagged -> recover",
			lastSeen:     now - 30,
			hasHeartbeat: true,
			createdAt:    now - 100000,
			alarmed:      true,
			want:         decisionRecover,
		},
		{
			name:         "never connected, still within grace -> none",
			hasHeartbeat: false,
			createdAt:    now - (grace - 60),
			alarmed:      false,
			want:         decisionNone,
		},
		{
			name:         "never connected, past grace -> alarm",
			hasHeartbeat: false,
			createdAt:    now - (grace + 60),
			alarmed:      false,
			want:         decisionAlarm,
		},
		{
			name:         "never connected past grace, already alarmed -> none",
			hasHeartbeat: false,
			createdAt:    now - (grace + 60),
			alarmed:      true,
			want:         decisionNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateSource(now, tc.lastSeen, tc.hasHeartbeat, tc.createdAt, tc.alarmed, silence, grace)
			if got != tc.want {
				t.Errorf("evaluateSource() = %d, want %d", got, tc.want)
			}
		})
	}
}
