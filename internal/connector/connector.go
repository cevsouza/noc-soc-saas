// Package connector holds the pluggable integrations that turn a monitoring/security tool's
// native alert format into a model.UnifiedIncident. Two lifecycles exist side by side:
//
//   - WebhookConnector: stateless, no network I/O. Given a raw JSON payload and a tenant, it
//     maps into zero or more UnifiedIncidents. Safe to call synchronously from an HTTP handler
//     (the typed /api/v1/ingest/* endpoints) or asynchronously from the raw-webhook mapping
//     engine (internal/worker) — the same implementation serves both call sites.
//   - PollConnector: stateful and long-running. It has no inbound webhook, so it pulls from an
//     external API on its own schedule and pushes mapped incidents onto the queue itself (see
//     internal/connector/sentinel.go).
package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// WebhookConnector maps a raw payload for one integration type into UnifiedIncidents. A slice
// return lets a single call cover both batch sources (Alertmanager-style, many alerts per
// request) and single-incident sources uniformly.
type WebhookConnector interface {
	Type() model.IncidentSource
	MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error)
}

// PollConnector runs its own background polling loop against an external API instead of
// receiving webhooks (e.g. Microsoft Sentinel).
type PollConnector interface {
	Start(ctx context.Context, interval time.Duration)
	Stop()
	TestConnection(ctx context.Context, tenantID uuid.UUID) error
}

var registry = map[model.IncidentSource]WebhookConnector{}

// Register adds a WebhookConnector to the registry, keyed by its Type(). Intended to be called
// from each connector implementation's init() function only — the registry is populated once
// at package load and never mutated afterward, so concurrent reads via Get need no locking.
func Register(c WebhookConnector) {
	registry[c.Type()] = c
}

// Get looks up a registered WebhookConnector by its integration-type string (case-insensitive,
// matching the lowercase model.IncidentSource values). Returns ok=false for unknown types,
// letting the caller fall back to generic best-effort handling.
func Get(sourceType string) (WebhookConnector, bool) {
	c, ok := registry[model.IncidentSource(strings.ToLower(sourceType))]
	return c, ok
}

// MustGet looks up a registered WebhookConnector by a known-at-compile-time source. Panics if
// unregistered — only used for the typed /api/v1/ingest/* handlers, which each correspond 1:1
// to a connector file that registers itself via init(); a panic here means that file's init()
// was removed but the handler wiring wasn't, a programming error rather than a runtime
// condition, so it's fine to fail loudly instead of returning a generic 500.
func MustGet(source model.IncidentSource) WebhookConnector {
	c, ok := Get(string(source))
	if !ok {
		panic(fmt.Sprintf("connector: no WebhookConnector registered for source %q", source))
	}
	return c
}

// MapRawPayload resolves a WebhookConnector for integrationType and maps payload through it.
// Unknown integration types fall back to a generic best-effort passthrough incident instead of
// a registered connector, since there's no real mapping logic for an unrecognized tool. Shared
// by internal/worker's async mapping engine and internal/api's DLQ replay endpoint so both go
// through identical dispatch logic instead of drifting apart.
func MapRawPayload(integrationType string, payload map[string]interface{}, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	if conn, ok := Get(integrationType); ok {
		return conn.MapToUnified(payloadBytes, tenantID)
	}

	incident := model.UnifiedIncident{
		ID:         uuid.New(),
		TenantID:   tenantID,
		Source:     model.IncidentSource(integrationType),
		EventType:  "generic_webhook_event",
		Severity:   model.SeverityInfo,
		Title:      "Generic Ingested Alert",
		Timestamp:  time.Now(),
		RawPayload: payload,
	}
	return []model.UnifiedIncident{incident}, nil
}
