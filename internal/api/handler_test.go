// Package api_test previously exercised the ingest handlers end-to-end (JSON decode + mapping
// + Redis push) against a possibly-unavailable local Redis. That approach never actually
// compiled against the real handler signatures (they take a *pgxpool.Pool the tests never
// passed) and, now that payload mapping has moved into internal/connector (see
// internal/connector/connector_test.go, which covers the happy-path and malformed-JSON cases
// for every registered connector as pure unit tests with no network dependency), there's
// nothing left in these handlers that's meaningfully unit-testable without a live Postgres
// connection — every one of them queries tenant_integrations before doing anything else.
//
// What IS still verifiable without a live Postgres connection is that each handler rejects
// non-POST methods before ever touching the database, since that check runs first.
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"noc-api/internal/api"

	"github.com/redis/go-redis/v9"
)

func TestIngestHandlersRejectNonPostMethods(t *testing.T) {
	// nil pool/client is safe here: every handler's method check runs before either is
	// dereferenced, so a GET request never reaches the code path that would need them.
	var redisClient *redis.Client

	handlers := map[string]http.HandlerFunc{
		"prometheus": api.HandlePrometheusIngest(nil, redisClient),
		"wazuh":      api.HandleWazuhIngest(nil, redisClient),
		"uptimekuma": api.HandleUptimeKumaIngest(nil, redisClient),
		"grafana":    api.HandleGrafanaIngest(nil, redisClient),
		"zabbix":     api.HandleZabbixIngest(nil, redisClient),
		"generic":    api.HandleIngest(nil, redisClient),
	}

	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/ingest/"+name, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405 Method Not Allowed for GET, got %d", rec.Code)
			}
		})
	}
}

