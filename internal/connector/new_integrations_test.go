package connector_test

import (
	"testing"

	"noc-api/internal/connector"
	"noc-api/internal/model"

	"github.com/google/uuid"
)

const otlpBatchJSON = `{
	"resourceLogs": [
		{
			"resource": {"attributes": [{"key": "host.name", "value": {"stringValue": "web-01"}}]},
			"scopeLogs": [
				{
					"logRecords": [
						{"timeUnixNano": "1750000000000000000", "severityNumber": 9, "severityText": "INFO", "body": {"stringValue": "routine info log"}},
						{"timeUnixNano": "1750000001000000000", "severityNumber": 17, "severityText": "ERROR", "body": {"stringValue": "database connection failed"}},
						{"timeUnixNano": "1750000002000000000", "severityNumber": 21, "severityText": "FATAL", "body": {"stringValue": "process crashed"}}
					]
				}
			]
		}
	]
}`

func TestOTLPConnectorFiltersBySeverity(t *testing.T) {
	conn := connector.MustGet(model.SourceOTLP)
	if conn.Type() != model.SourceOTLP {
		t.Errorf("expected Type() = %q, got %q", model.SourceOTLP, conn.Type())
	}

	incidents, err := conn.MapToUnified([]byte(otlpBatchJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the ERROR (17) and FATAL (21) records should survive; the INFO (9) record is
	// noise-filtered out — this is the core regression to guard.
	if len(incidents) != 2 {
		t.Fatalf("expected 2 incidents (INFO filtered out), got %d", len(incidents))
	}
	if incidents[0].Severity != model.SeverityCritical {
		t.Errorf("expected first surviving record (severityNumber=17) to map to critical, got %s", incidents[0].Severity)
	}
	if incidents[1].Severity != model.SeverityFatal {
		t.Errorf("expected second surviving record (severityNumber=21) to map to fatal, got %s", incidents[1].Severity)
	}
	if incidents[0].Host != "web-01" {
		t.Errorf("expected host from resource attributes, got %q", incidents[0].Host)
	}

	if _, err := conn.MapToUnified([]byte("not json"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

const icingaHostDownJSON = `{
	"check_type": "host",
	"host_name": "db-server-01",
	"state": "DOWN",
	"state_type": "HARD",
	"notification_type": "PROBLEM",
	"output": "CRITICAL - Host Unreachable",
	"long_date_time": "2026-07-11T10:00:00Z",
	"source": "icinga2"
}`

func TestIcingaConnector(t *testing.T) {
	conn := connector.MustGet(model.SourceIcinga)
	incidents, err := conn.MapToUnified([]byte(icingaHostDownJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical for state=DOWN, got %s", inc.Severity)
	}
	if inc.ExternalID != "db-server-01" {
		t.Errorf("expected external_id = host_name for host check, got %q", inc.ExternalID)
	}

	if _, err := conn.MapToUnified([]byte("{bad"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestIcingaConnectorServiceCheckExternalID(t *testing.T) {
	conn := connector.MustGet(model.SourceIcinga)
	body := `{"check_type":"service","host_name":"web-01","service_name":"HTTP","state":"CRITICAL","long_date_time":"2026-07-11T10:00:00Z"}`
	incidents, err := conn.MapToUnified([]byte(body), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if incidents[0].ExternalID != "web-01/HTTP" {
		t.Errorf("expected external_id = host/service for service check, got %q", incidents[0].ExternalID)
	}
}

const cloudwatchAlarmJSON = `{
	"AlarmName": "HighCPUUtilization",
	"AlarmDescription": "CPU above 90%",
	"AWSAccountId": "123456789012",
	"NewStateValue": "ALARM",
	"OldStateValue": "OK",
	"NewStateReason": "Threshold Crossed",
	"StateChangeTime": "2026-07-11T10:00:00Z",
	"Region": "us-east-1",
	"AlarmArn": "arn:aws:cloudwatch:us-east-1:123456789012:alarm:HighCPUUtilization",
	"Trigger": {
		"MetricName": "CPUUtilization",
		"Namespace": "AWS/EC2",
		"Dimensions": [{"name": "InstanceId", "value": "i-0abcd1234"}]
	}
}`

func TestCloudWatchConnector(t *testing.T) {
	conn := connector.MustGet(model.SourceCloudWatch)
	incidents, err := conn.MapToUnified([]byte(cloudwatchAlarmJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical for NewStateValue=ALARM, got %s", inc.Severity)
	}
	if inc.ExternalID != "arn:aws:cloudwatch:us-east-1:123456789012:alarm:HighCPUUtilization" {
		t.Errorf("expected external_id = AlarmArn, got %q", inc.ExternalID)
	}
	if inc.Host != "i-0abcd1234" {
		t.Errorf("expected host from InstanceId dimension, got %q", inc.Host)
	}

	if _, err := conn.MapToUnified([]byte("nope"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

const azureMonitorFiredJSON = `{
	"schemaId": "azureMonitorCommonAlertSchema",
	"data": {
		"essentials": {
			"alertId": "/subscriptions/xxx/alertId1",
			"originAlertId": "origin-alert-1",
			"alertRule": "High Memory Usage",
			"severity": "Sev1",
			"signalType": "Metric",
			"monitorCondition": "Fired",
			"monitoringService": "Platform",
			"alertTargetIDs": ["/subscriptions/xxx/resourceGroups/rg1/providers/Microsoft.Compute/vms/vm1"],
			"firedDateTime": "2026-07-11T10:00:00Z",
			"description": "Memory usage exceeded threshold"
		},
		"alertContext": {}
	}
}`

func TestAzureMonitorConnector(t *testing.T) {
	conn := connector.MustGet(model.SourceAzureMonitor)
	incidents, err := conn.MapToUnified([]byte(azureMonitorFiredJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical for Sev1, got %s", inc.Severity)
	}
	if inc.ExternalID != "origin-alert-1" {
		t.Errorf("expected external_id = originAlertId, got %q", inc.ExternalID)
	}
	if inc.EventType != "azure_monitor_fired" {
		t.Errorf("expected event_type azure_monitor_fired, got %q", inc.EventType)
	}

	if _, err := conn.MapToUnified([]byte("{{{"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

const pagerDutyTriggeredJSON = `{
	"event": {
		"event_type": "incident.triggered",
		"occurred_at": "2026-07-11T10:00:00Z",
		"data": {
			"id": "PD12345",
			"title": "Service is down",
			"status": "triggered",
			"urgency": "high"
		}
	}
}`

func TestPagerDutyConnector(t *testing.T) {
	conn := connector.MustGet(model.SourcePagerDuty)
	incidents, err := conn.MapToUnified([]byte(pagerDutyTriggeredJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityCritical {
		t.Errorf("expected severity critical for urgency=high, got %s", inc.Severity)
	}
	if inc.ExternalID != "PD12345" {
		t.Errorf("expected external_id = data.id, got %q", inc.ExternalID)
	}
	if inc.EventType != "pagerduty_triggered" {
		t.Errorf("expected event_type pagerduty_triggered, got %q", inc.EventType)
	}

	if _, err := conn.MapToUnified([]byte("bad json"), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

const opsgenieCreateJSON = `{
	"action": "Create",
	"alert": {
		"alertId": "OG98765",
		"message": "Disk space critical",
		"source": "monitoring-agent",
		"entity": "db-01",
		"priority": "P1",
		"tags": ["disk", "critical"]
	}
}`

func TestOpsgenieConnector(t *testing.T) {
	conn := connector.MustGet(model.SourceOpsgenie)
	incidents, err := conn.MapToUnified([]byte(opsgenieCreateJSON), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incidents))
	}
	inc := incidents[0]
	if inc.Severity != model.SeverityFatal {
		t.Errorf("expected severity fatal for priority=P1, got %s", inc.Severity)
	}
	if inc.ExternalID != "OG98765" {
		t.Errorf("expected external_id = alert.alertId, got %q", inc.ExternalID)
	}
	if inc.Host != "db-01" {
		t.Errorf("expected host = alert.entity, got %q", inc.Host)
	}

	if _, err := conn.MapToUnified([]byte("]not json["), uuid.New()); err == nil {
		t.Error("expected error for malformed JSON")
	}
}
