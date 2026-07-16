// Package ocsfsink streams OCSF Detection Findings out to a per-tenant HTTP sink (a security data
// lake, SIEM or XDR ingestion endpoint) in real time, as alerts are created. It mirrors
// internal/notifier's design: deliberately vault/DB agnostic — the caller (internal/worker) resolves
// and decrypts the per-tenant sink URL and passes it in, so this package only ever builds and sends
// one HTTP request given an already-resolved URL and an already-built ocsf.DetectionFinding. That
// keeps it trivially testable with httptest.Server and no DB mocking.
//
// This is the streaming counterpart to the pull export at GET /api/v1/alerts/ocsf (Backlog B3).
package ocsfsink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"noc-api/internal/ocsf"
)

// IntegrationType is the tenant_integrations.type value that gates OCSF streaming for a tenant.
const IntegrationType = "ocsf"

// VaultKey is the vault secret_key under which a tenant stores its OCSF sink URL.
const VaultKey = "ocsf_sink_url"

// Emitter POSTs OCSF findings to a tenant's configured sink URL.
type Emitter struct {
	httpClient *http.Client
}

// NewEmitter builds an Emitter with a short outbound timeout (the emit is best-effort and must never
// block alert processing for long).
func NewEmitter() *Emitter {
	return &Emitter{httpClient: &http.Client{Timeout: 5 * time.Second}}
}

// Emit sends one OCSF Detection Finding to sinkURL as a JSON POST. Returns an error on transport
// failure or a non-2xx response. The caller treats emission as best-effort (logs and continues).
func (e *Emitter) Emit(ctx context.Context, sinkURL string, finding ocsf.DetectionFinding) error {
	body, err := json.Marshal(finding)
	if err != nil {
		return fmt.Errorf("marshal ocsf finding: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sinkURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build ocsf sink request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ocsf sink request failed: %w", err)
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ocsf sink returned status %d", resp.StatusCode)
	}
	return nil
}
