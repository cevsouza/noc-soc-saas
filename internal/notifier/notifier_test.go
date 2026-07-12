package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

func testAlert() *model.Alert {
	return &model.Alert{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		EventType:   "cpu",
		Severity:    model.SeverityCritical,
		Summary:     "High CPU load on web-01",
		CreatedAt:   time.Now(),
		Fingerprint: "abc123fingerprint",
	}
}

func TestPagerDutyNotifierSendsExpectedRequest(t *testing.T) {
	var receivedBody pagerDutyEventRequest
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	n := &PagerDutyNotifier{httpClient: server.Client(), baseURL: server.URL}
	alert := testAlert()

	if err := n.Notify(context.Background(), "test-routing-key", alert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected application/json content type, got %q", receivedContentType)
	}
	if receivedBody.RoutingKey != "test-routing-key" {
		t.Errorf("expected routing_key to be passed through, got %q", receivedBody.RoutingKey)
	}
	if receivedBody.EventAction != "trigger" {
		t.Errorf("expected event_action=trigger, got %q", receivedBody.EventAction)
	}
	if receivedBody.Payload.Severity != "critical" {
		t.Errorf("expected severity=critical for model.SeverityCritical, got %q", receivedBody.Payload.Severity)
	}
	if receivedBody.Payload.Summary != alert.Summary {
		t.Errorf("expected summary to match alert.Summary, got %q", receivedBody.Payload.Summary)
	}
	if n.IntegrationType() != "pagerduty" {
		t.Errorf("expected IntegrationType()=pagerduty, got %q", n.IntegrationType())
	}
}

func TestPagerDutyNotifierFatalMapsToCritical(t *testing.T) {
	var receivedBody pagerDutyEventRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	n := &PagerDutyNotifier{httpClient: server.Client(), baseURL: server.URL}
	alert := testAlert()
	alert.Severity = model.SeverityFatal

	if err := n.Notify(context.Background(), "key", alert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody.Payload.Severity != "critical" {
		t.Errorf("expected fatal to map to PD's critical (no fatal enum value), got %q", receivedBody.Payload.Severity)
	}
}

func TestPagerDutyNotifierNonAcceptedStatusIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	n := &PagerDutyNotifier{httpClient: server.Client(), baseURL: server.URL}
	if err := n.Notify(context.Background(), "bad-key", testAlert()); err == nil {
		t.Error("expected error for non-202 response")
	}
}

func TestOpsgenieNotifierSendsExpectedRequest(t *testing.T) {
	var receivedBody opsgenieAlertRequest
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	n := &OpsgenieNotifier{httpClient: server.Client(), baseURL: server.URL}
	alert := testAlert()

	if err := n.Notify(context.Background(), "test-api-key", alert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedAuth != "GenieKey test-api-key" {
		t.Errorf("expected Authorization header 'GenieKey test-api-key', got %q", receivedAuth)
	}
	if receivedBody.Priority != "P2" {
		t.Errorf("expected priority P2 for model.SeverityCritical, got %q", receivedBody.Priority)
	}
	if receivedBody.Alias != alert.Fingerprint {
		t.Errorf("expected alias to reuse alert.Fingerprint for Opsgenie-side dedupe, got %q", receivedBody.Alias)
	}
	if n.IntegrationType() != "opsgenie" {
		t.Errorf("expected IntegrationType()=opsgenie, got %q", n.IntegrationType())
	}
}

func TestOpsgenieNotifierFatalMapsToP1(t *testing.T) {
	var receivedBody opsgenieAlertRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	n := &OpsgenieNotifier{httpClient: server.Client(), baseURL: server.URL}
	alert := testAlert()
	alert.Severity = model.SeverityFatal

	if err := n.Notify(context.Background(), "key", alert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody.Priority != "P1" {
		t.Errorf("expected P1 for fatal severity, got %q", receivedBody.Priority)
	}
}

func TestOpsgenieNotifierNonAcceptedStatusIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	n := &OpsgenieNotifier{httpClient: server.Client(), baseURL: server.URL}
	if err := n.Notify(context.Background(), "bad-key", testAlert()); err == nil {
		t.Error("expected error for non-202 response")
	}
}

func TestSlackNotifierSendsExpectedRequest(t *testing.T) {
	var receivedBody slackWebhookPayload
	var receivedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := &SlackNotifier{httpClient: server.Client()}
	alert := testAlert()

	if err := n.Notify(context.Background(), server.URL, alert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected application/json content type, got %q", receivedContentType)
	}
	if !strings.Contains(receivedBody.Text, alert.Summary) {
		t.Errorf("expected text to contain alert.Summary, got %q", receivedBody.Text)
	}
	if n.IntegrationType() != "slack" {
		t.Errorf("expected IntegrationType()=slack, got %q", n.IntegrationType())
	}
}

func TestSlackNotifierNonOKStatusIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	n := &SlackNotifier{httpClient: server.Client()}
	if err := n.Notify(context.Background(), server.URL, testAlert()); err == nil {
		t.Error("expected error for non-200 response")
	}
}

func TestTeamsNotifierSendsExpectedRequest(t *testing.T) {
	var receivedBody teamsMessageCard

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	n := &TeamsNotifier{httpClient: server.Client()}
	alert := testAlert()

	if err := n.Notify(context.Background(), server.URL, alert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedBody.Type != "MessageCard" {
		t.Errorf("expected @type=MessageCard, got %q", receivedBody.Type)
	}
	if len(receivedBody.Sections) == 0 {
		t.Fatal("expected at least one section")
	}
	found := false
	for _, f := range receivedBody.Sections[0].Facts {
		if f.Name == "Tenant" && f.Value == alert.TenantID.String() {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a Tenant fact matching alert.TenantID, got %+v", receivedBody.Sections[0].Facts)
	}
	if n.IntegrationType() != "teams" {
		t.Errorf("expected IntegrationType()=teams, got %q", n.IntegrationType())
	}
}

func TestTeamsNotifierThemeColorBySeverity(t *testing.T) {
	if got := teamsThemeColor(model.SeverityFatal); got != "FF0000" {
		t.Errorf("expected fatal to map to FF0000, got %q", got)
	}
	if got := teamsThemeColor(model.SeverityWarning); got != "FFA500" {
		t.Errorf("expected warning to map to FFA500, got %q", got)
	}
}

func TestTeamsNotifierNonOKStatusIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	n := &TeamsNotifier{httpClient: server.Client()}
	if err := n.Notify(context.Background(), server.URL, testAlert()); err == nil {
		t.Error("expected error for non-200 response")
	}
}
