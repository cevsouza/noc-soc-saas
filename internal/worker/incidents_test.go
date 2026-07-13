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
