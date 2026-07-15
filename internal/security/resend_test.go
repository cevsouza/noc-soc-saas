package security

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"os"
	"testing"
)

func setEnv(t *testing.T, k, v string) {
	t.Helper()
	old, existed := os.LookupEnv(k)
	os.Setenv(k, v)
	t.Cleanup(func() {
		if existed {
			os.Setenv(k, old)
		} else {
			os.Unsetenv(k)
		}
	})
}

func TestSendMailPrefersResend(t *testing.T) {
	var gotAuth, gotContentType string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	oldURL := resendAPIURL
	resendAPIURL = srv.URL
	defer func() { resendAPIURL = oldURL }()

	setEnv(t, "RESEND_API_KEY", "re_test_key")
	setEnv(t, "RESEND_FROM", "alerts@noc.example.com")
	// SMTP also configured — Resend must win and SMTP must NOT be called.
	setSMTPEnv(t)
	smtpCalled := false
	oldSMTP := sendMailFunc
	sendMailFunc = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		smtpCalled = true
		return nil
	}
	defer func() { sendMailFunc = oldSMTP }()

	if err := SendMail("dest@example.com", "Assunto", "<p>corpo</p>"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if smtpCalled {
		t.Error("SMTP was used despite RESEND_API_KEY being set — Resend must take precedence")
	}
	if gotAuth != "Bearer re_test_key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer re_test_key")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["from"] != "alerts@noc.example.com" {
		t.Errorf("from = %v, want alerts@noc.example.com", gotBody["from"])
	}
	if gotBody["subject"] != "Assunto" || gotBody["html"] != "<p>corpo</p>" {
		t.Errorf("subject/html mismatch: %v", gotBody)
	}
	to, ok := gotBody["to"].([]interface{})
	if !ok || len(to) != 1 || to[0] != "dest@example.com" {
		t.Errorf("to = %v, want [dest@example.com]", gotBody["to"])
	}
}

func TestSendMailResendNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"domain not verified"}`))
	}))
	defer srv.Close()

	oldURL := resendAPIURL
	resendAPIURL = srv.URL
	defer func() { resendAPIURL = oldURL }()

	setEnv(t, "RESEND_API_KEY", "re_test_key")

	if err := SendMail("dest@example.com", "Assunto", "<p>corpo</p>"); err == nil {
		t.Fatal("expected an error on a non-2xx Resend response, got nil")
	}
}

func TestSendMailResendDefaultFrom(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	oldURL := resendAPIURL
	resendAPIURL = srv.URL
	defer func() { resendAPIURL = oldURL }()

	setEnv(t, "RESEND_API_KEY", "re_test_key")
	os.Unsetenv("RESEND_FROM")

	if err := SendMail("dest@example.com", "Assunto", "<p>corpo</p>"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["from"] != "onboarding@resend.dev" {
		t.Errorf("default from = %v, want onboarding@resend.dev", gotBody["from"])
	}
}
