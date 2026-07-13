package worker

import "testing"

func TestWorseSeverity(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"info", "critical", "critical"},
		{"critical", "info", "critical"},
		{"warning", "fatal", "fatal"},
		{"fatal", "warning", "fatal"},
		{"critical", "critical", "critical"},
		{"warning", "info", "warning"},
		{"unknown", "warning", "warning"}, // unknown ranks lowest
		{"info", "unknown", "info"},
	}
	for _, tc := range cases {
		if got := worseSeverity(tc.a, tc.b); got != tc.want {
			t.Errorf("worseSeverity(%q,%q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestComputeRiskScore(t *testing.T) {
	cases := []struct {
		severity  string
		recurrence int
		want       int
	}{
		{"info", 1, 13},       // 10 base + 3
		{"critical", 1, 53},   // 50 base + 3
		{"fatal", 1, 73},      // 70 base + 3
		{"warning", 5, 45},    // 30 base + 15
		{"fatal", 20, 100},    // 70 + capped 30 = 100
		{"critical", 20, 80},  // 50 + capped 30
		{"info", 0, 13},       // recurrence floored to 1
		{"unknown", 1, 3},     // unknown severity base 0 + 3
	}
	for _, tc := range cases {
		if got := computeRiskScore(tc.severity, tc.recurrence); got != tc.want {
			t.Errorf("computeRiskScore(%q,%d) = %d, want %d", tc.severity, tc.recurrence, got, tc.want)
		}
	}
	// A recurring critical must outrank a one-off critical (the whole point of "dynamic").
	if computeRiskScore("critical", 8) <= computeRiskScore("critical", 1) {
		t.Error("recurring critical should score higher than a one-off critical")
	}
}
