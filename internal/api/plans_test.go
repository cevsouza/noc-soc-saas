package api

import "testing"

func TestResolvePlanUsesPresetWhenLimitsOmitted(t *testing.T) {
	p, err := resolvePlan("pro", 0, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := planPresets["pro"]
	if p != want {
		t.Fatalf("expected preset %+v, got %+v", want, p)
	}
}

func TestResolvePlanCustomOverride(t *testing.T) {
	p, err := resolvePlan("starter", 123456, 7, unlimited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.PlanName != "starter" || p.MaxAlertsPerMonth != 123456 || p.MaxIntegrations != 7 || p.MaxUsers != unlimited {
		t.Fatalf("custom override not preserved: %+v", p)
	}
}

func TestResolvePlanRejectsUnknownName(t *testing.T) {
	if _, err := resolvePlan("platinum", 0, 0, 0); err == nil {
		t.Fatal("expected error for unknown plan_name")
	}
}

func TestResolvePlanRejectsBadCustomLimit(t *testing.T) {
	// A negative other than the -1 unlimited sentinel is invalid.
	if _, err := resolvePlan("free", 100, -5, 3); err == nil {
		t.Fatal("expected error for limit < -1")
	}
}

func TestUtilizationPct(t *testing.T) {
	cases := []struct {
		used  int64
		limit int
		want  float64
	}{
		{50, 100, 50},
		{100, 100, 100},
		{150, 100, 150},
		{10, unlimited, 0}, // unlimited → 0
		{10, 0, 0},         // unset → 0
	}
	for _, c := range cases {
		if got := utilizationPct(c.used, c.limit); got != c.want {
			t.Errorf("utilizationPct(%d,%d)=%v want %v", c.used, c.limit, got, c.want)
		}
	}
}

func TestIsOverLimit(t *testing.T) {
	if !isOverLimit(101, 100) {
		t.Error("101/100 should be over limit")
	}
	if isOverLimit(100, 100) {
		t.Error("100/100 should not be over limit")
	}
	if isOverLimit(999, unlimited) {
		t.Error("unlimited should never be over limit")
	}
}

func TestDefaultTenantPlanIsFree(t *testing.T) {
	if defaultTenantPlan().PlanName != "free" {
		t.Fatalf("expected free default, got %q", defaultTenantPlan().PlanName)
	}
}
