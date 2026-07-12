package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/google/uuid"
)

func TestIsValidSNSSubscribeURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"valid AWS SNS host", "https://sns.us-east-1.amazonaws.com/?Action=ConfirmSubscription&Token=abc", true},
		{"valid AWS SNS host different region", "https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription", true},
		{"forged host", "https://evil.example.com/steal-me", false},
		{"host that merely contains amazonaws.com as a suffix trick", "https://sns.us-east-1.amazonaws.com.evil.com/", false},
		{"non-https scheme", "http://sns.us-east-1.amazonaws.com/", false},
		{"malformed URL", "://not a url", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidSNSSubscribeURL(tc.url)
			if got != tc.want {
				t.Errorf("isValidSNSSubscribeURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestHandleSNSSubscriptionConfirmationRejectsForgedHost(t *testing.T) {
	hit := false
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer trap.Close()

	envelope := snsEnvelope{
		Type:         "SubscriptionConfirmation",
		SubscribeURL: trap.URL, // host is 127.0.0.1:PORT, does not match sns.*.amazonaws.com
		TopicArn:     "arn:aws:sns:us-east-1:123456789012:test-topic",
	}

	rec := httptest.NewRecorder()
	handleSNSSubscriptionConfirmation(rec, uuid.New(), envelope)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for forged SubscribeURL host, got %d", rec.Code)
	}
	if hit {
		t.Error("expected the forged SubscribeURL to never be fetched (SSRF guard failed)")
	}
}

func TestHandleSNSSubscriptionConfirmationFetchesValidHost(t *testing.T) {
	hit := false
	// Use a TLS server since isValidSNSSubscribeURL requires https:// — plain
	// httptest.NewServer only gives an http:// URL, which real SNS SubscribeURLs never are.
	trap := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer trap.Close()

	// Temporarily relax the host pattern to accept the local test server's address, and swap
	// the confirm client for one that trusts the test server's self-signed cert, so we can
	// exercise the "valid host is fetched" path without hitting real AWS infrastructure.
	originalPattern := snsSubscribeURLHost
	originalClient := snsConfirmClient
	snsSubscribeURLHost = regexp.MustCompile(`^127\.0\.0\.1:\d+$`)
	snsConfirmClient = trap.Client()
	defer func() {
		snsSubscribeURLHost = originalPattern
		snsConfirmClient = originalClient
	}()

	envelope := snsEnvelope{
		Type:         "SubscriptionConfirmation",
		SubscribeURL: trap.URL,
		TopicArn:     "arn:aws:sns:us-east-1:123456789012:test-topic",
	}

	rec := httptest.NewRecorder()
	handleSNSSubscriptionConfirmation(rec, uuid.New(), envelope)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for a valid, matching SubscribeURL host, got %d", rec.Code)
	}
	if !hit {
		t.Error("expected the valid SubscribeURL to be fetched")
	}
}
