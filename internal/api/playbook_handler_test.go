package api

import (
	"testing"

	"noc-api/internal/playbook"
)

// validatePlaybookSteps layers a semantic check (does the vendor support the action?) on top of the
// pure structural validation. Uses the live responder registry (populated by internal/responder's
// init), so no DB is needed.
func TestValidatePlaybookSteps(t *testing.T) {
	cases := []struct {
		name    string
		steps   []playbook.Step
		wantErr bool
	}{
		{
			name:    "notify + supported action + comment",
			steps:   []playbook.Step{{Type: playbook.StepNotify, Channel: "slack"}, {Type: playbook.StepResponseAction, IntegrationType: "paloalto", ActionType: "block_ip", TargetFrom: "src_ip"}, {Type: playbook.StepComment, Text: "ok"}},
			wantErr: false,
		},
		{
			name:    "unsupported vendor/action combo",
			steps:   []playbook.Step{{Type: playbook.StepResponseAction, IntegrationType: "paloalto", ActionType: "contain_host", Target: "1.2.3.4"}}, // firewalls don't contain hosts
			wantErr: true,
		},
		{
			name:    "unknown integration",
			steps:   []playbook.Step{{Type: playbook.StepResponseAction, IntegrationType: "nope", ActionType: "block_ip", Target: "1.2.3.4"}},
			wantErr: true,
		},
		{
			name:    "structural failure still caught",
			steps:   []playbook.Step{{Type: "bogus"}},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePlaybookSteps(c.steps)
			if (err != nil) != c.wantErr {
				t.Errorf("validatePlaybookSteps() err=%v, wantErr=%v", err, c.wantErr)
			}
		})
	}
}
