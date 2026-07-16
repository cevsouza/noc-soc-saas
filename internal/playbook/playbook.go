// Package playbook holds the pure, dependency-free model for the multi-step SOAR playbook engine
// (Backlog B7): the step vocabulary, structural validation, target resolution, and the rule for
// which steps need human approval. The actual execution (DB, responder, notifier) lives in the api
// layer; keeping this package a pure leaf makes the model trivially unit-testable.
package playbook

import (
	"fmt"
	"strings"
)

// Step type discriminators.
const (
	StepNotify         = "notify"          // page a channel (auto-executes)
	StepComment        = "comment"         // post an incident comment (auto-executes)
	StepResponseAction = "response_action" // vendor containment — mutates state, needs approval
)

// Step is one entry in a playbook's ordered sequence. Only the fields relevant to its Type are set.
type Step struct {
	Type string `json:"type"`

	// notify
	Channel string `json:"channel,omitempty"` // slack, teams, email, pagerduty, opsgenie
	Message string `json:"message,omitempty"` // optional override; falls back to the incident title

	// comment
	Text string `json:"text,omitempty"`

	// response_action
	IntegrationType string `json:"integration_type,omitempty"` // paloalto, fortinet, crowdstrike
	ActionType      string `json:"action_type,omitempty"`      // block_ip, contain_host, ...
	Target          string `json:"target,omitempty"`           // literal target (IP / host id)
	TargetFrom      string `json:"target_from,omitempty"`      // OR: key to read from the run context
}

// notifyChannels is the set of channels a notify step may target (mirrors internal/notifier +
// the escalation vault keys wired in the worker).
var notifyChannels = map[string]bool{
	"slack": true, "teams": true, "email": true, "pagerduty": true, "opsgenie": true,
}

// StepNeedsApproval reports whether a step mutates external network/endpoint state and therefore
// must pause the run for human approval. Only containment (response_action) does; notify/comment
// are side-effect-light and auto-execute.
func StepNeedsApproval(stepType string) bool {
	return stepType == StepResponseAction
}

// ValidateSteps checks a playbook definition structurally (known types, required fields present),
// without touching the responder registry or the DB. Pure and unit-tested. Semantic checks that a
// vendor actually supports an action are done at the api layer against the live responder registry.
func ValidateSteps(steps []Step) error {
	if len(steps) == 0 {
		return fmt.Errorf("playbook must have at least one step")
	}
	for i, s := range steps {
		switch s.Type {
		case StepNotify:
			if !notifyChannels[strings.TrimSpace(s.Channel)] {
				return fmt.Errorf("step %d: unknown notify channel %q", i, s.Channel)
			}
		case StepComment:
			if strings.TrimSpace(s.Text) == "" {
				return fmt.Errorf("step %d: comment text is required", i)
			}
		case StepResponseAction:
			if strings.TrimSpace(s.IntegrationType) == "" || strings.TrimSpace(s.ActionType) == "" {
				return fmt.Errorf("step %d: integration_type and action_type are required", i)
			}
			if strings.TrimSpace(s.Target) == "" && strings.TrimSpace(s.TargetFrom) == "" {
				return fmt.Errorf("step %d: target or target_from is required", i)
			}
		default:
			return fmt.Errorf("step %d: unknown step type %q", i, s.Type)
		}
	}
	return nil
}

// ResolveTarget returns a response_action step's effective target: the literal Target when set,
// otherwise the value from the run context under TargetFrom. Pure.
func ResolveTarget(s Step, context map[string]string) (string, error) {
	if t := strings.TrimSpace(s.Target); t != "" {
		return t, nil
	}
	if v, ok := context[s.TargetFrom]; ok && strings.TrimSpace(v) != "" {
		return v, nil
	}
	return "", fmt.Errorf("target_from %q not present in run context", s.TargetFrom)
}
