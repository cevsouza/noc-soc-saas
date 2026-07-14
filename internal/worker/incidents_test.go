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
		severity    string
		recurrence  int
		criticality string
		want        int
	}{
		{"info", 1, "", 13},          // 10 base + 3, no asset
		{"critical", 1, "", 53},      // 50 base + 3
		{"fatal", 1, "", 73},         // 70 base + 3
		{"warning", 5, "", 45},       // 30 base + 15
		{"fatal", 20, "", 100},       // 70 + capped 30 = 100
		{"critical", 20, "", 80},     // 50 + capped 30
		{"info", 0, "", 13},          // recurrence floored to 1
		{"unknown", 1, "", 3},        // unknown severity base 0 + 3
		{"critical", 1, "low", 53},   // low criticality is neutral (no bonus)
		{"critical", 1, "medium", 53},// medium is the default, no bonus
		{"critical", 1, "high", 68},  // 53 + 15 high-asset bonus
		{"critical", 1, "critical", 78}, // 53 + 25 critical-asset bonus
		{"fatal", 20, "critical", 100},  // already capped, criticality can't exceed 100
		{"unknown", 1, "high", 18},   // 0 base + 3 + 15
	}
	for _, tc := range cases {
		if got := computeRiskScore(tc.severity, tc.recurrence, tc.criticality); got != tc.want {
			t.Errorf("computeRiskScore(%q,%d,%q) = %d, want %d", tc.severity, tc.recurrence, tc.criticality, got, tc.want)
		}
	}
	// A recurring critical must outrank a one-off critical (the whole point of "dynamic").
	if computeRiskScore("critical", 8, "") <= computeRiskScore("critical", 1, "") {
		t.Error("recurring critical should score higher than a one-off critical")
	}
	// The same alert on a critical asset must outrank it on an ordinary host (the point of B1).
	if computeRiskScore("critical", 1, "critical") <= computeRiskScore("critical", 1, "") {
		t.Error("critical asset should score higher than an ordinary host")
	}
}
