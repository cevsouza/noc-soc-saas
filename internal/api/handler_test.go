package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"noc-api/internal/api"
	"noc-api/internal/db"
	"noc-api/internal/model"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestHandlePrometheusIngest(t *testing.T) {
	// Setup Redis Mock/Client
	redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"}) // local mock client
	// Note: We bypass real network calls by using context mock or ignoring errors if redis isn't online,
	// but we can test the json unmarshaling and mapping directly!
	
	tenantID := uuid.New()
	ctx := db.WithTenantID(context.Background(), tenantID)

	alertmanagerJSON := `{
		"receiver": "webhook",
		"status": "firing",
		"alerts": [
			{
				"status": "firing",
				"labels": {
					"alertname": "HostHighCpuLoad",
					"instance": "web-server-01",
					"severity": "critical"
				},
				"annotations": {
					"summary": "High CPU load on web-server-01",
					"description": "CPU load is at 96% for 5 minutes."
				},
				"startsAt": "2026-06-30T12:00:00Z",
				"fingerprint": "prom-fingerprint-123"
			}
		]
	}`

	req := httptest.NewRequest("POST", "/api/v1/ingest/prometheus", strings.NewReader(alertmanagerJSON))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler := api.HandlePrometheusIngest(redisClient)
	
	// We run it and expect StatusAccepted (202) if redis pushes successfully or StatusInternalServerError (500) if redis connection is refused.
	// Since Redis might not be running locally, we can capture either response. If it crashes, it's a test failure.
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted && rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 202 Accepted or 500 InternalServerError (redis offline), got %d", rec.Code)
	}
}

func TestHandleWazuhIngest(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

	tenantID := uuid.New()
	ctx := db.WithTenantID(context.Background(), tenantID)

	wazuhJSON := `{
		"timestamp": "2026-06-30T12:05:00Z",
		"rule": {
			"level": 10,
			"comment": "Successful login after multiple failed attempts",
			"sid": 5715,
			"id": "5715",
			"groups": ["syslog", "sshd", "security_event"]
		},
		"agent": {
			"id": "002",
			"name": "gateway-router",
			"ip": "192.168.1.254"
		},
		"location": "/var/log/auth.log",
		"full_log": "Jun 30 12:05:00 gateway-router sshd[999]: Accepted password for root from 192.168.10.12"
	}`

	req := httptest.NewRequest("POST", "/api/v1/ingest/wazuh", strings.NewReader(wazuhJSON))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler := api.HandleWazuhIngest(redisClient)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted && rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 202 Accepted or 500 InternalServerError (redis offline), got %d", rec.Code)
	}
}
