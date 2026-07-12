// Package notifier makes outbound escalation calls to paging vendors (PagerDuty, Opsgenie)
// when one of the platform's own alerts needs a human paged. It is deliberately vault/DB
// agnostic — callers (internal/worker) resolve and decrypt the per-tenant secret and pass it
// in, so this package only ever builds and sends an HTTP request given an already-decrypted
// secret and a *model.Alert. That keeps it trivially testable with httptest.Server and no DB
// mocking.
package notifier

import (
	"context"

	"noc-api/internal/model"
)

// Escalator sends one alert to an external paging/incident-management vendor.
type Escalator interface {
	// IntegrationType matches the tenant_integrations.type value that gates this escalator
	// (e.g. "pagerduty", "opsgenie").
	IntegrationType() string
	// Notify sends alert to the vendor using secret (an already-decrypted credential value —
	// a routing key, API key, etc., depending on the vendor). Returns an error if the vendor
	// call fails or returns a non-success status.
	Notify(ctx context.Context, secret string, alert *model.Alert) error
}
