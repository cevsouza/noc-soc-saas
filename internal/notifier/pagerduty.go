package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"noc-api/internal/model"
)

const pagerDutyEventsAPIURL = "https://events.pagerduty.com/v2/enqueue"

type pagerDutyEventPayload struct {
	Summary       string                 `json:"summary"`
	Source        string                 `json:"source"`
	Severity      string                 `json:"severity"`
	Timestamp     string                 `json:"timestamp"`
	CustomDetails map[string]interface{} `json:"custom_details,omitempty"`
}

type pagerDutyEventRequest struct {
	RoutingKey  string                `json:"routing_key"`
	EventAction string                `json:"event_action"`
	Payload     pagerDutyEventPayload `json:"payload"`
}

// PagerDutyNotifier escalates critical/fatal alerts via PagerDuty's Events API v2.
type PagerDutyNotifier struct {
	httpClient *http.Client
	baseURL    string
}

func NewPagerDutyNotifier() *PagerDutyNotifier {
	return &PagerDutyNotifier{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    pagerDutyEventsAPIURL,
	}
}

func (n *PagerDutyNotifier) IntegrationType() string { return "pagerduty" }

func (n *PagerDutyNotifier) Notify(ctx context.Context, routingKey string, alert *model.Alert) error {
	// PagerDuty's severity enum is {critical, error, warning, info} — no "fatal". This
	// notifier only ever fires for model.SeverityCritical/SeverityFatal (see the gate in
	// internal/worker/worker.go), so both map to PD's "critical".
	severity := "critical"
	if alert.Severity == model.SeverityWarning {
		severity = "warning"
	} else if alert.Severity == model.SeverityInfo {
		severity = "info"
	}

	reqBody := pagerDutyEventRequest{
		RoutingKey:  routingKey,
		EventAction: "trigger",
		Payload: pagerDutyEventPayload{
			Summary:   alert.Summary,
			Source:    "noc-saas",
			Severity:  severity,
			Timestamp: alert.CreatedAt.Format(time.RFC3339),
			CustomDetails: map[string]interface{}{
				"tenant_id":   alert.TenantID.String(),
				"event_type":  alert.EventType,
				"fingerprint": alert.Fingerprint,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal PagerDuty event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build PagerDuty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PagerDuty API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("PagerDuty API returned unexpected status %d", resp.StatusCode)
	}
	return nil
}
