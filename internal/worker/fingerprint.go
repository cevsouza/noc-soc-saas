package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// debouncePointer is what's actually stored in Redis at the debounce key: enough to fetch the
// original alert row back via AlertRepository.GetByID, which requires an exact match on
// created_at for partition pruning on the range-partitioned alerts table.
type debouncePointer struct {
	AlertID   uuid.UUID `json:"alert_id"`
	CreatedAt time.Time `json:"created_at"`
}

// computeFingerprint derives a stable content hash used to correlate repeat occurrences of the
// same underlying incident. It trusts ExternalID first (Prometheus/Alertmanager's own
// fingerprint, Wazuh's rule ID, Sentinel's incident name, Zabbix's event ID, and UptimeKuma's
// monitor ID) since that's the most precise identity each source tool can offer. When
// ExternalID is empty (e.g. manually submitted incidents via HandleIngest), it falls back to a
// tenant+source+device+event_type+title seed.
//
// This does NOT attempt any cross-tool semantic correlation — e.g. Prometheus's "HighCPU" and
// Zabbix's "cpu_load_high" firing for the same underlying host condition still produce
// different fingerprints. That's a much harder correlation problem, intentionally out of scope
// here.
func computeFingerprint(event model.UnifiedIncident) string {
	var seed string
	if event.ExternalID != "" {
		seed = strings.Join([]string{event.TenantID.String(), string(event.Source), event.ExternalID}, "|")
	} else {
		deviceKey := "nil_device"
		if event.DeviceID != nil {
			deviceKey = event.DeviceID.String()
		}
		seed = strings.Join([]string{
			event.TenantID.String(),
			string(event.Source),
			deviceKey,
			event.EventType,
			strings.ToLower(strings.TrimSpace(event.Title)),
		}, "|")
	}
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
