package queue

import "github.com/google/uuid"

// RawWebhookMessage is the envelope pushed onto AlertsRawQueueKey by HandleGenericWebhook. The
// payload's shape depends on IntegrationType, which is only known at runtime (it's a
// tenant-configured string), so mapping is deferred to the async mapping engine.
type RawWebhookMessage struct {
	TenantID        uuid.UUID              `json:"tenant_id"`
	IntegrationType string                 `json:"integration_type"`
	Payload         map[string]interface{} `json:"payload"`
}
