package worker

import (
	"testing"
	"time"
)

func TestRuleMatches(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	before := now.Add(-2 * time.Hour)
	after := now.Add(2 * time.Hour)
	fields := map[string]string{"event_type": "disk_full", "host": "web-prod-01", "summary": "Disco cheio", "source": "zabbix", "severity": "warning"}

	cases := []struct {
		name string
		rule suppressionRule
		want bool
	}{
		{"substring match on event_type", suppressionRule{MatchField: "event_type", MatchValue: "disk"}, true},
		{"case-insensitive", suppressionRule{MatchField: "host", MatchValue: "WEB-PROD"}, true},
		{"no match", suppressionRule{MatchField: "host", MatchValue: "db-node"}, false},
		{"unknown field", suppressionRule{MatchField: "nope", MatchValue: "x"}, false},
		{"empty value never matches", suppressionRule{MatchField: "host", MatchValue: ""}, false},
		{"within window", suppressionRule{MatchField: "event_type", MatchValue: "disk", StartsAt: &before, EndsAt: &after}, true},
		{"before window starts", suppressionRule{MatchField: "event_type", MatchValue: "disk", StartsAt: &after}, false},
		{"after window ends", suppressionRule{MatchField: "event_type", MatchValue: "disk", EndsAt: &before}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ruleMatches(tc.rule, fields, now); got != tc.want {
				t.Errorf("ruleMatches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEventSuppressed(t *testing.T) {
	now := time.Now()
	fields := map[string]string{"event_type": "cpu_high", "host": "h1", "summary": "s", "source": "zabbix", "severity": "critical"}
	// No rules -> not suppressed.
	if eventSuppressed(nil, fields, now) {
		t.Error("no rules should not suppress")
	}
	// One non-matching + one matching -> suppressed.
	rules := []suppressionRule{
		{MatchField: "host", MatchValue: "other"},
		{MatchField: "event_type", MatchValue: "cpu"},
	}
	if !eventSuppressed(rules, fields, now) {
		t.Error("a matching rule should suppress")
	}
}
