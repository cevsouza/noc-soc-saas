package retention

import (
	"testing"
	"time"
)

func TestValidateDays(t *testing.T) {
	if err := ValidateDays(30); err != nil {
		t.Fatalf("30 should be valid: %v", err)
	}
	if err := ValidateDays(3650); err != nil {
		t.Fatalf("3650 should be valid: %v", err)
	}
	if ValidateDays(29) == nil {
		t.Fatal("29 must be rejected (below floor)")
	}
	if ValidateDays(3651) == nil {
		t.Fatal("3651 must be rejected (above max)")
	}
	if ValidateDays(0) == nil {
		t.Fatal("0 must be rejected")
	}
}

func TestCutoffClampsToFloor(t *testing.T) {
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

	// 90 days -> cutoff exactly 90 days back.
	if got := Cutoff(now, 90); !got.Equal(now.AddDate(0, 0, -90)) {
		t.Fatalf("Cutoff(90) = %v, want %v", got, now.AddDate(0, 0, -90))
	}
	// Below the floor is clamped up to MinDays, so it never deletes anything younger than 30 days.
	if got := Cutoff(now, 5); !got.Equal(now.AddDate(0, 0, -MinDays)) {
		t.Fatalf("Cutoff(5) = %v, want floor %v", got, now.AddDate(0, 0, -MinDays))
	}
	// The cutoff must always be strictly in the past.
	if !Cutoff(now, 30).Before(now) {
		t.Fatal("cutoff must be before now")
	}
}
