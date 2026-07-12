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

// teamsMessageCard uses the classic MessageCard schema (not Adaptive Card) because Microsoft
// Teams' classic "Incoming Webhook" connector — what a tenant-configured webhook URL implies
// here — renders MessageCard natively, whereas Adaptive Cards need an `attachments` envelope
// that not every webhook configuration accepts.
type teamsMessageCard struct {
	Type       string                `json:"@type"`
	Context    string                `json:"@context"`
	ThemeColor string                `json:"themeColor"`
	Summary    string                `json:"summary"`
	Sections   []teamsMessageSection `json:"sections"`
}

type teamsMessageSection struct {
	ActivityTitle string      `json:"activityTitle"`
	Facts         []teamsFact `json:"facts"`
}

type teamsFact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// TeamsNotifier escalates critical/fatal alerts via a Microsoft Teams incoming webhook. Like
// Slack, the "secret" is the webhook URL itself.
type TeamsNotifier struct {
	httpClient *http.Client
}

func NewTeamsNotifier() *TeamsNotifier {
	return &TeamsNotifier{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (n *TeamsNotifier) IntegrationType() string { return "teams" }

func teamsThemeColor(severity model.AlertSeverity) string {
	switch severity {
	case model.SeverityFatal, model.SeverityCritical:
		return "FF0000"
	case model.SeverityWarning:
		return "FFA500"
	default:
		return "0078D7"
	}
}

func (n *TeamsNotifier) Notify(ctx context.Context, webhookURL string, alert *model.Alert) error {
	card := teamsMessageCard{
		Type:       "MessageCard",
		Context:    "http://schema.org/extensions",
		ThemeColor: teamsThemeColor(alert.Severity),
		Summary:    alert.Summary,
		Sections: []teamsMessageSection{
			{
				ActivityTitle: alert.Summary,
				Facts: []teamsFact{
					{Name: "Severidade", Value: string(alert.Severity)},
					{Name: "Tenant", Value: alert.TenantID.String()},
					{Name: "Tipo de Evento", Value: alert.EventType},
					{Name: "Fingerprint", Value: alert.Fingerprint},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("failed to marshal Teams payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build Teams request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Teams webhook call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Teams webhook returned unexpected status %d", resp.StatusCode)
	}
	return nil
}
