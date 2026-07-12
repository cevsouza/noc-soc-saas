package api

import "testing"

func TestComputeNoiseRatio(t *testing.T) {
	cases := []struct {
		total, distinct int
		want            float64
	}{
		{0, 0, 0},     // no data
		{10, 0, 0},    // guard: distinct 0 must not divide-by-zero
		{10, 10, 1.0}, // no dedupe
		{30, 10, 3.0}, // 3 alerts per incident
		{5, 2, 2.5},
	}
	for _, c := range cases {
		if got := computeNoiseRatio(c.total, c.distinct); got != c.want {
			t.Errorf("computeNoiseRatio(%d, %d) = %v, want %v", c.total, c.distinct, got, c.want)
		}
	}
}

func TestEstimateHoursSaved(t *testing.T) {
	// 15 min per automation → 4 automations = 1h.
	if got := estimateHoursSaved(0, 0); got != 0 {
		t.Errorf("estimateHoursSaved(0,0) = %v, want 0", got)
	}
	if got := estimateHoursSaved(4, 0); got != 1.0 {
		t.Errorf("estimateHoursSaved(4,0) = %v, want 1.0", got)
	}
	if got := estimateHoursSaved(2, 2); got != 1.0 {
		t.Errorf("estimateHoursSaved(2,2) = %v, want 1.0", got)
	}
	if got := estimateHoursSaved(8, 4); got != 3.0 {
		t.Errorf("estimateHoursSaved(8,4) = %v, want 3.0", got)
	}
}
