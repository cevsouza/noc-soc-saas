package connector

import (
	"encoding/json"
	"fmt"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

type UptimeKumaPayload struct {
	Heartbeat struct {
		MonitorID int    `json:"monitorID"`
		Status    int    `json:"status"` // 0 = Down, 1 = Up
		Time      string `json:"time"`
		Msg       string `json:"msg"`
	} `json:"heartbeat"`
	Monitor struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		Url      string `json:"url"`
		Type     string `json:"type"`
	} `json:"monitor"`
	Msg string `json:"msg"`
}

type uptimeKumaConnector struct{}

func init() {
	Register(uptimeKumaConnector{})
}

func (uptimeKumaConnector) Type() model.IncidentSource { return model.SourceUptimeKuma }

func (uptimeKumaConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var payload UptimeKumaPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, err
	}

	severity := model.SeverityInfo
	if payload.Heartbeat.Status == 0 {
		severity = model.SeverityCritical
	}

	rawMap := make(map[string]interface{})
	rawMap["heartbeat"] = payload.Heartbeat
	rawMap["monitor"] = payload.Monitor
	rawMap["msg"] = payload.Msg

	timestamp, err := time.Parse("2006-01-02 15:04:05.000", payload.Heartbeat.Time)
	if err != nil {
		timestamp = time.Now()
	}

	host := payload.Monitor.Hostname
	if host == "" {
		host = payload.Monitor.Url
	}

	incident := model.UnifiedIncident{
		ID:       uuid.New(),
		TenantID: tenantID,
		Source:   model.SourceUptimeKuma,
		// NOTE: intentionally just the monitor ID, not "<id>_<unixTime>" as before — the old
		// per-second timestamp suffix made every heartbeat "unique," which defeated
		// fingerprint-based dedupe (every repeat of the same monitor-down event looked like a
		// brand new incident). Dropping it lets repeated heartbeats for the same monitor share
		// a fingerprint like every other source. This changes the value surfaced in
		// AIAnalysis.external_id compared to before.
		ExternalID:  fmt.Sprintf("%d", payload.Monitor.ID),
		EventType:   payload.Monitor.Type,
		Severity:    severity,
		Title:       payload.Msg,
		Description: payload.Heartbeat.Msg,
		Host:        host,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
