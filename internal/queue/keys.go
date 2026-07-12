// Package queue holds the Redis queue key constants and message shapes shared by the
// ingestion pipeline (internal/api), the connector registry (internal/connector), and the
// background worker (internal/worker). It intentionally has no internal dependencies so
// internal/connector can use these constants without importing internal/api (which would
// create an import cycle, since internal/worker already imports internal/api).
package queue

const (
	// AlertsRawQueueKey holds raw, not-yet-mapped webhook payloads for integration types whose
	// shape isn't known at compile time (see HandleGenericWebhook). Consumed by the async
	// mapping engine in internal/worker, which resolves a Connector and pushes the mapped
	// result onto AlertsNormalizedQueueKey.
	AlertsRawQueueKey = "noc:queue:alerts:raw"

	// AlertsNormalizedQueueKey holds fully-mapped model.UnifiedIncident JSON, ready for
	// internal/worker's main processing loop (dedupe + persistence + SOAR triggers). Every
	// producer that already knows how to map its own payload (the typed /api/v1/ingest/*
	// handlers, HandleIngest, the Sentinel poll connector, and the mapping engine) pushes here.
	AlertsNormalizedQueueKey = "noc:queue:alerts:normalized"

	// AlertsDLQQueueKey holds entries that failed to parse or map, wrapped as DLQEntry. Capped
	// at MaxDLQSize via PushToDLQ so it cannot grow unbounded.
	AlertsDLQQueueKey = "noc:queue:alerts:dlq"

	// AlertsPoisonQueueKey holds DLQ entries that have already been replayed MaxDLQRetries
	// times and still fail — parked here instead of retried forever, so a persistently broken
	// tenant integration surfaces for a human to fix instead of looping silently.
	AlertsPoisonQueueKey = "noc:queue:alerts:poison"

	// MaxDLQSize caps the DLQ list length (oldest entries trimmed first) to bound memory use.
	MaxDLQSize = 5000

	// MaxDLQRetries is how many times a DLQ entry may be replayed before it is moved to the
	// poison queue instead of being retried again.
	MaxDLQRetries = 2
)
