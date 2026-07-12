package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"noc-api/internal/model"
)

type slackWebhookPayload struct {
	Text string `json:"text"`
}

// SlackNotifier escalates critical/fatal alerts via a Slack incoming webhook. Unlike
// PagerDuty/Opsgenie's API-key-based auth, the "secret" here is the webhook URL itself —
// Slack's incoming webhooks are pre-authorized by URL, no separate credential needed.
type SlackNotifier struct {
	httpClient *http.Client
}

func NewSlackNotifier() *SlackNotifier {
	return &SlackNotifier{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (n *SlackNotifier) IntegrationType() string { return "slack" }

func (n *SlackNotifier) Notify(ctx context.Context, webhookURL string, alert *model.Alert) error {
	text := fmt.Sprintf(
		"🚨 *[%s]* %s\n*Tenant:* %s | *Tipo:* %s",
		strings.ToUpper(string(alert.Severity)), alert.Summary, alert.TenantID, alert.EventType,
	)

	bodyBytes, err := json.Marshal(slackWebhookPayload{Text: text})
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build Slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Slack webhook call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Slack webhook returned unexpected status %d", resp.StatusCode)
	}
	return nil
}
