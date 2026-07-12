// Package responder makes outbound *containment* calls to security vendors — blocking a source
// IP on a firewall (Palo Alto, Fortinet) or containing/isolating a host on an EDR (CrowdStrike).
// It is the write-side counterpart to internal/connector (which ingests events from those same
// vendors) and, like internal/notifier, it is deliberately vault/DB agnostic: callers
// (internal/api) resolve and decrypt the per-tenant credentials and pass them in as a plain
// map, so this package only ever builds and sends HTTP requests given already-decrypted secrets.
// That keeps every vendor trivially testable with httptest.Server and no DB mocking.
//
// Every action here mutates network/endpoint state, so callers MUST gate execution behind the
// human approval flow (see internal/api/response_handler.go + response_action_requests) — this
// package intentionally has no notion of "auto-execute".
package responder

import (
	"context"
	"fmt"
)

// ActionType enumerates the vendor-native containment actions the platform can request.
type ActionType string

const (
	ActionBlockIP         ActionType = "block_ip"         // firewall: add IP to a blocklist
	ActionUnblockIP       ActionType = "unblock_ip"       // firewall: remove IP from the blocklist
	ActionContainHost     ActionType = "contain_host"     // EDR: network-isolate a device
	ActionLiftContainment ActionType = "lift_containment" // EDR: release a device from isolation
)

// Action is a single requested containment operation. Target is an IP address for firewall
// actions and a device/host id for EDR actions.
type Action struct {
	Type   ActionType
	Target string
}

// Responder performs a vendor-native containment action given already-decrypted per-tenant
// credentials (an api key, an oauth client id/secret, a base URL, etc., depending on the
// vendor — the caller supplies whatever keys that vendor documents). Execute returns a short
// human-readable output string describing what happened (recorded in the audit trail) and an
// error if the vendor call fails, a required credential is missing, or the action is unsupported.
type Responder interface {
	// IntegrationType matches the tenant_integrations.type value that gates this responder
	// (e.g. "paloalto", "fortinet", "crowdstrike").
	IntegrationType() string
	// SupportedActions lists the actions this vendor can perform (firewalls block IPs, EDRs
	// contain hosts — not every vendor does every action).
	SupportedActions() []ActionType
	Execute(ctx context.Context, creds map[string]string, action Action) (output string, err error)
}

var registry = map[string]Responder{}

// Register wires a responder into the lookup table at package-init time (each vendor file calls
// this from its init()). Not concurrency-guarded: all writes happen during package load.
func Register(r Responder) { registry[r.IntegrationType()] = r }

// Get returns the responder registered for an integration type, if any.
func Get(integrationType string) (Responder, bool) {
	r, ok := registry[integrationType]
	return r, ok
}

// Supports reports whether the given integration type is registered and can perform the action.
func Supports(integrationType string, action ActionType) bool {
	r, ok := registry[integrationType]
	if !ok {
		return false
	}
	for _, a := range r.SupportedActions() {
		if a == action {
			return true
		}
	}
	return false
}

// requireCred fetches a required credential from the map, returning a clear error (surfaced to
// the operator as a failed action, never a panic) when it is missing or blank.
func requireCred(creds map[string]string, key string) (string, error) {
	v := creds[key]
	if v == "" {
		return "", fmt.Errorf("missing required credential %q in vault for this integration", key)
	}
	return v, nil
}

// credOrDefault returns creds[key] when present and non-blank, otherwise def.
func credOrDefault(creds map[string]string, key, def string) string {
	if v := creds[key]; v != "" {
		return v
	}
	return def
}
