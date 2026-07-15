package api

import "testing"

func TestCoveragePct(t *testing.T) {
	cases := []struct {
		covered, total int
		want           float64
	}{
		{0, 0, 0},      // no discovered devices
		{5, 0, 0},      // guard: total 0 must not divide-by-zero
		{0, 10, 0},     // nothing covered
		{5, 10, 50.0},  // half covered
		{10, 10, 100.0}, // fully covered
		{3, 4, 75.0},
	}
	for _, c := range cases {
		if got := coveragePct(c.covered, c.total); got != c.want {
			t.Errorf("coveragePct(%d, %d) = %v, want %v", c.covered, c.total, got, c.want)
		}
	}
}
