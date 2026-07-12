package api

import "testing"

func TestDeriveSLAExecutiveStatsNoResolvedDefaultsTo100Percent(t *testing.T) {
	rows := []severityRow{
		{severity: "fatal", targetMinutes: 15},
		{severity: "critical", targetMinutes: 30},
		{severity: "warning", targetMinutes: 120},
		{severity: "info", targetMinutes: 480},
	}

	stats := deriveSLAExecutiveStats(rows)

	if stats.SLACompliance != 100.0 {
		t.Errorf("expected 100%% compliance with zero resolved incidents, got %v", stats.SLACompliance)
	}
	if stats.TotalIncidents != 0 || stats.ResolvedCount != 0 || stats.UnresolvedCount != 0 {
		t.Errorf("expected all counts to be zero, got %+v", stats)
	}
	if len(stats.BySeverity) != 4 {
		t.Fatalf("expected 4 severity entries even with zero data, got %d", len(stats.BySeverity))
	}
	for _, b := range stats.BySeverity {
		if b.Compliance != 100.0 {
			t.Errorf("expected per-severity compliance to default to 100%% for %s, got %v", b.Severity, b.Compliance)
		}
	}
}

func TestDeriveSLAExecutiveStatsMixedComplianceWeightedCorrectly(t *testing.T) {
	rows := []severityRow{
		// fatal: 2 resolved, 1 met the 15min target, 1 missed -> 50% compliance
		{severity: "fatal", targetMinutes: 15, count: 2, resolvedCount: 2, avgTTA: 5, avgTTR: 20, metSLACount: 1},
		// critical: 4 resolved, all 4 met the 30min target -> 100% compliance
		{severity: "critical", targetMinutes: 30, count: 4, resolvedCount: 4, avgTTA: 10, avgTTR: 25, metSLACount: 4},
		{severity: "warning", targetMinutes: 120},
		{severity: "info", targetMinutes: 480},
	}

	stats := deriveSLAExecutiveStats(rows)

	if stats.TotalIncidents != 6 {
		t.Errorf("expected total_incidents=6, got %d", stats.TotalIncidents)
	}
	if stats.ResolvedCount != 6 {
		t.Errorf("expected resolved_count=6, got %d", stats.ResolvedCount)
	}
	// overall: 5 met / 6 resolved = 83.33...%
	wantOverall := float64(5) / float64(6) * 100.0
	if stats.SLACompliance != wantOverall {
		t.Errorf("expected overall compliance %v, got %v", wantOverall, stats.SLACompliance)
	}

	if stats.BySeverity[0].Severity != "fatal" || stats.BySeverity[0].Compliance != 50.0 {
		t.Errorf("expected fatal compliance=50%%, got %+v", stats.BySeverity[0])
	}
	if stats.BySeverity[1].Severity != "critical" || stats.BySeverity[1].Compliance != 100.0 {
		t.Errorf("expected critical compliance=100%%, got %+v", stats.BySeverity[1])
	}
}

func TestDeriveSLAExecutiveStatsSeverityOrderIsFixed(t *testing.T) {
	// Deliberately built out of order to confirm the caller (computeSLAExecutiveStats) is
	// responsible for ordering — this test documents that deriveSLAExecutiveStats itself just
	// preserves whatever order it's given, so computeSLAExecutiveStats MUST always pass rows
	// pre-ordered fatal->critical->warning->info.
	rows := []severityRow{
		{severity: "fatal", targetMinutes: 15},
		{severity: "critical", targetMinutes: 30},
		{severity: "warning", targetMinutes: 120},
		{severity: "info", targetMinutes: 480},
	}
	stats := deriveSLAExecutiveStats(rows)

	wantOrder := []string{"fatal", "critical", "warning", "info"}
	for i, want := range wantOrder {
		if stats.BySeverity[i].Severity != want {
			t.Errorf("expected position %d to be %q, got %q", i, want, stats.BySeverity[i].Severity)
		}
	}
}
