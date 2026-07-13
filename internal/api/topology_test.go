package api

import "testing"

func TestSeverityRankToString(t *testing.T) {
	cases := []struct {
		rank int
		want string
	}{
		{4, "fatal"},
		{3, "critical"},
		{2, "warning"},
		{1, "info"},
		{0, ""},
		{-1, ""},
		{99, ""},
	}
	for _, tc := range cases {
		if got := severityRankToString(tc.rank); got != tc.want {
			t.Errorf("severityRankToString(%d) = %q, want %q", tc.rank, got, tc.want)
		}
	}
}
