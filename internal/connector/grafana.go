package connector

import (
	"noc-api/internal/model"

	"github.com/google/uuid"
)

type grafanaConnector struct{}

func init() {
	Register(grafanaConnector{})
}

func (grafanaConnector) Type() model.IncidentSource { return model.SourceGrafana }

// Grafana's alerting webhook payload matches Prometheus Alertmanager's shape verbatim, so this
// just delegates to the shared batch mapper and stamps the Grafana source.
func (grafanaConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	return mapAlertmanagerBatch(rawPayload, tenantID, model.SourceGrafana)
}
