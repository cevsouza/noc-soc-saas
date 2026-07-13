package api

import "testing"

func TestComputeEPS(t *testing.T) {
	// 30 days of seconds.
	window := 30.0 * 86400

	if got := computeEPS(0, window); got != 0 {
		t.Fatalf("computeEPS(0) = %v, want 0", got)
	}
	// 2,592,000 events over 30 days = exactly 1 EPS.
	if got := computeEPS(2592000, window); got != 1 {
		t.Fatalf("computeEPS(2592000, 30d) = %v, want 1", got)
	}
	// Zero/negative window never divides by zero.
	if got := computeEPS(100, 0); got != 0 {
		t.Fatalf("computeEPS with zero window = %v, want 0", got)
	}
	// Monotonic: more events -> higher EPS.
	if computeEPS(100, window) <= computeEPS(50, window) {
		t.Fatal("EPS should increase with event count")
	}
}
