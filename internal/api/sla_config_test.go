package api

import "testing"

func TestMergeSLATargets(t *testing.T) {
	// No overrides → all four defaults returned unchanged.
	base := mergeSLATargets(nil)
	if len(base) != 4 {
		t.Fatalf("expected 4 severities, got %d", len(base))
	}
	if base["critical"].MTTRTargetMinutes != 30 {
		t.Errorf("default critical MTTR = %v, want 30", base["critical"].MTTRTargetMinutes)
	}

	// A tenant override replaces only that severity; others keep defaults.
	merged := mergeSLATargets(map[string]SLATarget{
		"critical": {Severity: "critical", MTTATargetMinutes: 3, MTTRTargetMinutes: 12},
	})
	if merged["critical"].MTTRTargetMinutes != 12 || merged["critical"].MTTATargetMinutes != 3 {
		t.Errorf("critical override not applied: %+v", merged["critical"])
	}
	if merged["fatal"].MTTRTargetMinutes != 15 {
		t.Errorf("fatal should keep default 15, got %v", merged["fatal"].MTTRTargetMinutes)
	}

	// Unknown severities in overrides are ignored (never introduces a 5th key).
	merged = mergeSLATargets(map[string]SLATarget{"bogus": {Severity: "bogus", MTTATargetMinutes: 1, MTTRTargetMinutes: 2}})
	if len(merged) != 4 {
		t.Errorf("unknown severity leaked into merged targets: %d keys", len(merged))
	}
}

func TestValidateSLATarget(t *testing.T) {
	if err := validateSLATarget(SLATarget{Severity: "critical", MTTATargetMinutes: 10, MTTRTargetMinutes: 30}); err != nil {
		t.Errorf("valid target rejected: %v", err)
	}
	bad := []SLATarget{
		{Severity: "nope", MTTATargetMinutes: 10, MTTRTargetMinutes: 30}, // invalid severity
		{Severity: "critical", MTTATargetMinutes: 0, MTTRTargetMinutes: 30}, // non-positive mtta
		{Severity: "critical", MTTATargetMinutes: 10, MTTRTargetMinutes: -1}, // non-positive mttr
		{Severity: "critical", MTTATargetMinutes: 40, MTTRTargetMinutes: 30}, // mtta > mttr
	}
	for _, tc := range bad {
		if err := validateSLATarget(tc); err == nil {
			t.Errorf("expected validation error for %+v", tc)
		}
	}
}
