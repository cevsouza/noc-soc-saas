package connector_test

import (
	"testing"

	"noc-api/internal/connector"
	"noc-api/internal/model"

	"github.com/google/uuid"
)

func TestRegistryGet(t *testing.T) {
	if _, ok := connector.Get("prometheus"); !ok {
		t.Error("expected prometheus connector to be registered")
	}
	if _, ok := connector.Get("PROMETHEUS"); !ok {
		t.Error("expected Get to be case-insensitive")
	}
	if _, ok := connector.Get("unknown_tool"); ok {
		t.Error("expected unknown_tool to not be registered")
	}
}

func TestMustGetPanicsForUnknownSource(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected MustGet to panic for an unregistered source")
		}
	}()
	connector.MustGet(model.IncidentSource("does_not_exist"))
}

const alertmanagerBatchJSON = `{
	"receiver": "webhook",
	"status": "firing",
	"alerts": [
		{
			"status": "firing",
			"labels": {"alertname": "HostHighCpuLoad", "instance": "web-server-01", "severity": "critical"},
			"annotations": {"summary": "High CPU load on web-server-01", "description": "CPU load is at 96%% for 5 minutes."},
			"startsAt": "2026-06-30T12:00:00Z",
			"fingerprint": "prom-fingerprint-123"
		}
	]
}`

func TestPrometheusConnector(t *testing.T) {
	conn := connector.MustGet(model.SourcePrometheus)
	if conn.Type() != model.SourcePrometheus {
		t.Errorf("expected Type() = %q, got %q", model.SourcePrometheus, conn.Type())
	}

	tenantID := uuid.New()
	incidents, err := conn.MapToUnified([]byte(alertmanagerBatchJSON), tenantID)
	if err != nil {
		t.Fatalf("unexpected error mapping valid Alertmanager batch: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Source != model.SourcePrometheus {
		t.Errorf("expected source prometheus, got %s", inc.Source)
	}
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical, got %s", inc.Severity)
	}
	if inc.ExternalID != "prom-fingerprint-123" {
		t.Errorf("expected external_id from fingerprint, got %q", inc.ExternalID)
	}
	if inc.TenantID != tenantID {
		t.Errorf("expected tenant id to be propagated")
	}

	if _, err := conn.MapToUnified([]byte("{not valid json"), tenantID); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestGrafanaConnectorDelegatesToPrometheusMapping(t *testing.T) {
	conn := connector.MustGet(model.SourceGrafana)
	if conn.Type() != model.SourceGrafana {
		t.Errorf("expected Type() = %q, got %q", model.SourceGrafana, conn.Type())
	}

	incidents, err := conn.MapToUnified([]byte(alertmanagerBatchJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	if incidents[0].Source != model.SourceGrafana {
		t.Errorf("expected source overwritten to grafana, got %s", incidents[0].Source)
	}

	if _, err := conn.MapToUnified([]byte("not json"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

const wazuhJSON = `{
	"timestamp": "2026-06-30T12:05:00Z",
	"rule": {"level": 10, "comment": "Successful login after multiple failed attempts", "sid": 5715, "id": "5715", "groups": ["syslog", "sshd", "security_event"]},
	"agent": {"id": "002", "name": "gateway-router", "ip": "192.168.1.254"},
	"location": "/var/log/auth.log",
	"full_log": "Jun 30 12:05:00 gateway-router sshd[999]: Accepted password for root from 192.168.10.12"
}`

func TestWazuhConnector(t *testing.T) {
	conn := connector.MustGet(model.SourceWazuh)
	incidents, err := conn.MapToUnified([]byte(wazuhJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical for rule level 10, got %s", inc.Severity)
	}
	if inc.Host != "192.168.1.254" {
		t.Errorf("expected host from agent.ip, got %q", inc.Host)
	}

	if _, err := conn.MapToUnified([]byte("{bad"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

const uptimeKumaJSON = `{
	"heartbeat": {"monitorID": 1, "status": 0, "time": "2026-06-30 12:00:00.000", "msg": "Connection timeout"},
	"monitor": {"id": 1, "name": "Google DNS", "hostname": "8.8.8.8", "url": "8.8.8.8", "type": "ping"},
	"msg": "[Google DNS] [Down] Connection timeout"
}`

func TestUptimeKumaConnector(t *testing.T) {
	conn := connector.MustGet(model.SourceUptimeKuma)

	incidents, err := conn.MapToUnified([]byte(uptimeKumaJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical for status=0 (down), got %s", inc.Severity)
	}
	// Regression check for the dedupe fix: ExternalID must be stable across repeat heartbeats
	// for the same monitor, i.e. it must NOT embed a per-second timestamp.
	if inc.ExternalID != "1" {
		t.Errorf("expected external_id to be just the monitor id \"1\", got %q", inc.ExternalID)
	}

	incidents2, err := conn.MapToUnified([]byte(uptimeKumaJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error on second mapping: %v", err)
	}
	if incidents2[0].ExternalID != incidents[0].ExternalID {
		t.Errorf("expected repeated heartbeats for the same monitor to share an external_id, got %q vs %q", incidents[0].ExternalID, incidents2[0].ExternalID)
	}

	if _, err := conn.MapToUnified([]byte("nope"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

const zabbixJSON = `{
	"alert_subject": "PROBLEM: High CPU load on host-xyz",
	"alert_message": "CPU load is 92%%",
	"host": "host-xyz",
	"severity": "high",
	"trigger_id": "12345",
	"event_id": "98765",
	"event_value": "1"
}`

func TestZabbixConnector(t *testing.T) {
	conn := connector.MustGet(model.SourceZabbix)
	incidents, err := conn.MapToUnified([]byte(zabbixJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical for zabbix severity=high, got %s", inc.Severity)
	}
	if inc.ExternalID != "98765" {
		t.Errorf("expected external_id from event_id, got %q", inc.ExternalID)
	}

	if _, err := conn.MapToUnified([]byte("{{{"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestMapRawPayloadFallsBackForUnknownIntegration(t *testing.T) {
	tenantID := uuid.New()
	incidents, err := connector.MapRawPayload("some_unknown_tool", map[string]interface{}{"foo": "bar"}, tenantID)
	if err != nil {
		t.Fatalf("unexpected error for unknown integration fallback: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 fallback incident, got %d", len(incidents))
	}
	if incidents[0].Source != model.IncidentSource("some_unknown_tool") {
		t.Errorf("expected fallback source to echo the integration type, got %s", incidents[0].Source)
	}
}

func TestMapRawPayloadDispatchesToRegisteredConnector(t *testing.T) {
	tenantID := uuid.New()
	payload := map[string]interface{}{
		"alert_subject": "PROBLEM: test",
		"alert_message": "test",
		"host":          "host-1",
		"severity":      "disaster",
		"event_id":      "1",
	}
	incidents, err := connector.MapRawPayload("zabbix", payload, tenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 || incidents[0].Severity != model.SeverityFatal {
		t.Errorf("expected zabbix connector to handle dispatch and map severity=disaster to fatal, got %+v", incidents)
	}
}
