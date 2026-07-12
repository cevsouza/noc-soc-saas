package worker

import (
	"testing"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

func baseEvent() model.UnifiedIncident {
	return model.UnifiedIncident{
		TenantID:   uuid.New(),
		Source:     model.SourcePrometheus,
		ExternalID: "prom-fingerprint-123",
		EventType:  "HighCPU",
		Title:      "High CPU load",
		Timestamp:  time.Now(),
	}
}

func TestComputeFingerprintSameEventSameFingerprint(t *testing.T) {
	event := baseEvent()
	fp1 := computeFingerprint(event)
	fp2 := computeFingerprint(event)
	if fp1 != fp2 {
		t.Errorf("expected identical fingerprints for the same event, got %q vs %q", fp1, fp2)
	}
}

func TestComputeFingerprintIgnoresTimestamp(t *testing.T) {
	// Two occurrences of the same incident always differ in Timestamp — the fingerprint must
	// not be sensitive to it, or repeat events would never dedupe.
	event1 := baseEvent()
	event2 := event1
	event2.Timestamp = event1.Timestamp.Add(1 * time.Hour)

	if computeFingerprint(event1) != computeFingerprint(event2) {
		t.Error("expected fingerprint to be stable across differing Timestamp values")
	}
}

func TestComputeFingerprintDiffersByTenant(t *testing.T) {
	event1 := baseEvent()
	event2 := event1
	event2.TenantID = uuid.New()

	if computeFingerprint(event1) == computeFingerprint(event2) {
		t.Error("expected different tenants to produce different fingerprints")
	}
}

func TestComputeFingerprintDiffersBySource(t *testing.T) {
	event1 := baseEvent()
	event2 := event1
	event2.Source = model.SourceZabbix

	if computeFingerprint(event1) == computeFingerprint(event2) {
		t.Error("expected different sources to produce different fingerprints, even with the same external_id")
	}
}

func TestComputeFingerprintDiffersByExternalID(t *testing.T) {
	event1 := baseEvent()
	event2 := event1
	event2.ExternalID = "different-external-id"

	if computeFingerprint(event1) == computeFingerprint(event2) {
		t.Error("expected different external_id values to produce different fingerprints")
	}
}

func TestComputeFingerprintFallbackWhenExternalIDEmpty(t *testing.T) {
	deviceID := uuid.New()
	event1 := model.UnifiedIncident{
		TenantID:  uuid.New(),
		Source:    model.IncidentSource("generic"),
		DeviceID:  &deviceID,
		EventType: "manual_incident",
		Title:     "Manual Incident",
	}
	event2 := event1 // same fallback seed fields

	fp1 := computeFingerprint(event1)
	fp2 := computeFingerprint(event2)
	if fp1 != fp2 {
		t.Error("expected identical fallback fingerprints for identical tenant/source/device/event_type/title")
	}

	// Different title (case/whitespace only) should still collide, since the fallback
	// normalizes it.
	event3 := event1
	event3.Title = "  MANUAL incident  "
	if computeFingerprint(event1) != computeFingerprint(event3) {
		t.Error("expected fallback fingerprint to be case/whitespace-insensitive on title")
	}

	// A different device should NOT collide.
	otherDevice := uuid.New()
	event4 := event1
	event4.DeviceID = &otherDevice
	if computeFingerprint(event1) == computeFingerprint(event4) {
		t.Error("expected different device_id to produce a different fallback fingerprint")
	}

	// Nil DeviceID must not panic and must differ from a non-nil one.
	event5 := event1
	event5.DeviceID = nil
	fp5 := computeFingerprint(event5)
	if fp5 == fp1 {
		t.Error("expected nil device_id to produce a different fingerprint than a set device_id")
	}
}

func TestComputeFingerprintUptimeKumaRepeatsCollide(t *testing.T) {
	// Regression check for the dedupe fix: once UptimeKuma's ExternalID is just the monitor ID
	// (no embedded per-second timestamp — see internal/connector/uptimekuma.go), repeated
	// heartbeat-down events for the same monitor must share a fingerprint.
	tenantID := uuid.New()
	first := model.UnifiedIncident{
		TenantID:   tenantID,
		Source:     model.SourceUptimeKuma,
		ExternalID: "1",
		Timestamp:  time.Now(),
	}
	second := first
	second.Timestamp = first.Timestamp.Add(10 * time.Minute)

	if computeFingerprint(first) != computeFingerprint(second) {
		t.Error("expected repeated UptimeKuma heartbeats for the same monitor to share a fingerprint")
	}
}
