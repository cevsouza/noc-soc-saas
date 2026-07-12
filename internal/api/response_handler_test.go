package api

import "testing"

// TestValidateResponseActionRequest exercises the pure request-validation path (registry lookup
// + required-field checks) with no database, mirroring the connector/sla_stats pure-unit pattern.
func TestValidateResponseActionRequest(t *testing.T) {
	cases := []struct {
		name    string
		req     CreateResponseActionRequest
		wantErr bool
	}{
		{"paloalto block ok", CreateResponseActionRequest{IntegrationType: "paloalto", ActionType: "block_ip", Target: "1.2.3.4"}, false},
		{"fortinet unblock ok", CreateResponseActionRequest{IntegrationType: "fortinet", ActionType: "unblock_ip", Target: "1.2.3.4"}, false},
		{"crowdstrike contain ok", CreateResponseActionRequest{IntegrationType: "crowdstrike", ActionType: "contain_host", Target: "dev-1"}, false},
		{"trims whitespace", CreateResponseActionRequest{IntegrationType: "  paloalto ", ActionType: " block_ip ", Target: " 1.2.3.4 "}, false},
		{"missing target", CreateResponseActionRequest{IntegrationType: "paloalto", ActionType: "block_ip", Target: ""}, true},
		{"missing action", CreateResponseActionRequest{IntegrationType: "paloalto", ActionType: "", Target: "1.2.3.4"}, true},
		{"missing integration", CreateResponseActionRequest{IntegrationType: "", ActionType: "block_ip", Target: "1.2.3.4"}, true},
		{"unsupported action for vendor", CreateResponseActionRequest{IntegrationType: "paloalto", ActionType: "contain_host", Target: "1.2.3.4"}, true},
		{"unknown integration", CreateResponseActionRequest{IntegrationType: "nozzle", ActionType: "block_ip", Target: "1.2.3.4"}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := validateResponseActionRequest(c.req)
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
