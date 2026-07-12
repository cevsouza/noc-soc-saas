package connector

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// otlpLogsPayload mirrors the official JSON encoding of OTLP's ExportLogsServiceRequest
// (OTLP/HTTP, not gRPC/protobuf — this connector intentionally does not implement a full
// OTel-Collector-compatible receiver, only the lightweight JSON transport).
type otlpLogsPayload struct {
	ResourceLogs []struct {
		Resource struct {
			Attributes []otlpKeyValue `json:"attributes"`
		} `json:"resource"`
		ScopeLogs []struct {
			LogRecords []otlpLogRecord `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
}

type otlpLogRecord struct {
	TimeUnixNano   string         `json:"timeUnixNano"`
	SeverityNumber int            `json:"severityNumber"`
	SeverityText   string         `json:"severityText"`
	Body           otlpAnyValue   `json:"body"`
	Attributes     []otlpKeyValue `json:"attributes"`
}

type otlpAnyValue struct {
	StringValue string `json:"stringValue"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpConnector struct{}

func init() {
	Register(otlpConnector{})
}

func (otlpConnector) Type() model.IncidentSource { return model.SourceOTLP }

// otlpMinAlertSeverity is the noise-control threshold: OTLP's SeverityNumber ranges are
// 1-4 TRACE, 5-8 DEBUG, 9-12 INFO, 13-16 WARN, 17-20 ERROR, 21-24 FATAL. This is an alerting
// pipeline, not a log-aggregation sink, so only ERROR/FATAL (>=17) become incidents — TRACE
// through WARN are silently dropped rather than flooding the debounce engine with every log
// line. Fixed for now; not tenant-configurable (see plan's open question — a v2 could read a
// per-tenant threshold from tenant_integrations.settings JSONB without any schema change).
const otlpMinAlertSeverity = 17

func (otlpConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload otlpLogsPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}

	var incidents []model.UnifiedIncident
	for _, rl := range payload.ResourceLogs {
		host := ""
		for _, attr := range rl.Resource.Attributes {
			if attr.Key == "host.name" {
				host = attr.Value.StringValue
				break
			}
		}

		for _, sl := range rl.ScopeLogs {
			for _, rec := range sl.LogRecords {
				if rec.SeverityNumber < otlpMinAlertSeverity {
					continue
				}

				severity := model.SeverityCritical
				if rec.SeverityNumber >= 21 {
					severity = model.SeverityFatal
				}

				body := rec.Body.StringValue
				title := body
				if len(title) > 120 {
					title = title[:120]
				}
				if rec.SeverityText != "" {
					title = rec.SeverityText + " log: " + title
				}

				rawMap := make(map[string]interface{})
				rawMap["severity_text"] = rec.SeverityText
				rawMap["body"] = body
				attrs := make(map[string]string, len(rec.Attributes))
				for _, attr := range rec.Attributes {
					attrs[attr.Key] = attr.Value.StringValue
				}
				rawMap["attributes"] = attrs

				incidents = append(incidents, model.UnifiedIncident{
					ID:          uuid.New(),
					TenantID:    tenantID,
					Source:      model.SourceOTLP,
					EventType:   "otlp_log",
					Severity:    severity,
					Title:       title,
					Description: body,
					Host:        host,
					RawPayload:  rawMap,
					Timestamp:   parseOTLPTimestamp(rec.TimeUnixNano),
				})
			}
		}
	}

	return incidents, nil
}

func parseOTLPTimestamp(unixNanoStr string) time.Time {
	nanos, err := strconv.ParseInt(strings.TrimSpace(unixNanoStr), 10, 64)
	if err != nil || nanos == 0 {
		return time.Now()
	}
	return time.Unix(0, nanos)
}
