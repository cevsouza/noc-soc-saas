package playbook

import "testing"

func TestStepNeedsApproval(t *testing.T) {
	if !StepNeedsApproval(StepResponseAction) {
		t.Error("response_action must need approval")
	}
	if StepNeedsApproval(StepNotify) || StepNeedsApproval(StepComment) {
		t.Error("notify/comment must not need approval")
	}
}

func TestValidateSteps(t *testing.T) {
	cases := []struct {
		name    string
		steps   []Step
		wantErr bool
	}{
		{"empty", nil, true},
		{"good notify", []Step{{Type: StepNotify, Channel: "slack"}}, false},
		{"bad channel", []Step{{Type: StepNotify, Channel: "carrier-pigeon"}}, true},
		{"good comment", []Step{{Type: StepComment, Text: "done"}}, false},
		{"empty comment", []Step{{Type: StepComment, Text: "  "}}, true},
		{"good action literal", []Step{{Type: StepResponseAction, IntegrationType: "paloalto", ActionType: "block_ip", Target: "1.2.3.4"}}, false},
		{"good action from-context", []Step{{Type: StepResponseAction, IntegrationType: "fortinet", ActionType: "block_ip", TargetFrom: "src_ip"}}, false},
		{"action missing target", []Step{{Type: StepResponseAction, IntegrationType: "paloalto", ActionType: "block_ip"}}, true},
		{"action missing integration", []Step{{Type: StepResponseAction, ActionType: "block_ip", Target: "1.2.3.4"}}, true},
		{"unknown type", []Step{{Type: "launch_missiles"}}, true},
		{"multi valid", []Step{{Type: StepNotify, Channel: "teams"}, {Type: StepResponseAction, IntegrationType: "crowdstrike", ActionType: "contain_host", TargetFrom: "host"}, {Type: StepComment, Text: "contido"}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSteps(c.steps)
			if (err != nil) != c.wantErr {
				t.Errorf("ValidateSteps() err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestResolveTarget(t *testing.T) {
	ctx := map[string]string{"src_ip": "203.0.113.9", "host": "web-01"}

	if got, err := ResolveTarget(Step{Target: "10.0.0.1"}, ctx); err != nil || got != "10.0.0.1" {
		t.Errorf("literal target = %q/%v", got, err)
	}
	if got, err := ResolveTarget(Step{TargetFrom: "src_ip"}, ctx); err != nil || got != "203.0.113.9" {
		t.Errorf("from-context = %q/%v", got, err)
	}
	if _, err := ResolveTarget(Step{TargetFrom: "missing"}, ctx); err == nil {
		t.Error("expected error for missing context key")
	}
	// Literal takes precedence over target_from.
	if got, _ := ResolveTarget(Step{Target: "9.9.9.9", TargetFrom: "src_ip"}, ctx); got != "9.9.9.9" {
		t.Errorf("literal should win, got %q", got)
	}
}
