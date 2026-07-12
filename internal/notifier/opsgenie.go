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

const opsgenieAlertsAPIURL = "https://api.opsgenie.com/v2/alerts"

type opsgenieAlertRequest struct {
	Message     string `json:"message"`
	Alias       string `json:"alias,omitempty"`
	Description string `json:"description,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Source      string `json:"source,omitempty"`
}

// OpsgenieNotifier escalates critical/fatal alerts via Opsgenie's Alert API v2.
type OpsgenieNotifier struct {
	httpClient *http.Client
	baseURL    string
}

func NewOpsgenieNotifier() *OpsgenieNotifier {
	return &OpsgenieNotifier{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    opsgenieAlertsAPIURL,
	}
}

func (n *OpsgenieNotifier) IntegrationType() string { return "opsgenie" }

func (n *OpsgenieNotifier) Notify(ctx context.Context, apiKey string, alert *model.Alert) error {
	priority := "P2"
	if alert.Severity == model.SeverityFatal {
		priority = "P1"
	}

	reqBody := opsgenieAlertRequest{
		Message: alert.Summary,
		// Reuses the alert's own dedupe fingerprint as Opsgenie's alias, so Opsgenie's own
		// alert-deduplication correlates repeat escalations for the same underlying incident.
		Alias:       alert.Fingerprint,
		Description: alert.Summary,
		Priority:    priority,
		Source:      "noc-saas",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal Opsgenie alert: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build Opsgenie request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "GenieKey "+apiKey)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Opsgenie API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("Opsgenie API returned unexpected status %d", resp.StatusCode)
	}
	return nil
}
