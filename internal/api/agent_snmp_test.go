package api

import "testing"

func TestValidateSNMPTargetInput(t *testing.T) {
	valid := SNMPTargetInput{
		Name: "sw", Host: "10.0.0.1", Community: "public",
		Checks: []SNMPCheck{{OID: "1.2.3", Label: "cpu", Comparison: "gt", Threshold: 90, Severity: "critical"}},
	}
	if err := validateSNMPTargetInput(valid); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	bad := []SNMPTargetInput{
		{Host: "x", Community: "c", Checks: valid.Checks},                                                                   // no name
		{Name: "x", Community: "c", Checks: valid.Checks},                                                                   // no host
		{Name: "x", Host: "x", Checks: valid.Checks},                                                                        // no community
		{Name: "x", Host: "x", Community: "c"},                                                                              // no checks
		{Name: "x", Host: "x", Community: "c", Port: 70000, Checks: valid.Checks},                                           // bad port
		{Name: "x", Host: "x", Community: "c", Version: "3", Checks: valid.Checks},                                          // bad version
		{Name: "x", Host: "x", Community: "c", Checks: []SNMPCheck{{OID: "1", Label: "l", Comparison: "??", Severity: "critical"}}}, // bad comparison
		{Name: "x", Host: "x", Community: "c", Checks: []SNMPCheck{{OID: "1", Label: "l", Comparison: "gt", Severity: "??"}}},        // bad severity
		{Name: "x", Host: "x", Community: "c", Checks: []SNMPCheck{{OID: "", Label: "l", Comparison: "gt", Severity: "info"}}},       // no oid
	}
	for i, b := range bad {
		if validateSNMPTargetInput(b) == nil {
			t.Errorf("case %d should be rejected: %+v", i, b)
		}
	}
}
