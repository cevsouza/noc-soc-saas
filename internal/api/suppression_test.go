package api

import (
	"testing"
	"time"
)

func TestValidateCreateSuppressionRule(t *testing.T) {
	valid := CreateSuppressionRuleRequest{Name: "maint", MatchField: "host", MatchValue: "web-01"}
	if err := validateCreateSuppressionRule(valid); err != nil {
		t.Errorf("valid rule rejected: %v", err)
	}

	start := time.Now()
	end := start.Add(time.Hour)
	if err := validateCreateSuppressionRule(CreateSuppressionRuleRequest{Name: "w", MatchField: "host", MatchValue: "x", StartsAt: &start, EndsAt: &end}); err != nil {
		t.Errorf("valid windowed rule rejected: %v", err)
	}

	bad := []CreateSuppressionRuleRequest{
		{Name: "", MatchField: "host", MatchValue: "x"},               // empty name
		{Name: "n", MatchField: "nope", MatchValue: "x"},              // invalid field
		{Name: "n", MatchField: "host", MatchValue: ""},               // empty value
		{Name: "n", MatchField: "host", MatchValue: "x", StartsAt: &end, EndsAt: &start}, // end before start
	}
	for i, tc := range bad {
		if err := validateCreateSuppressionRule(tc); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}
