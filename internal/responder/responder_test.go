package responder

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient() *http.Client { return &http.Client{Timeout: 5 * time.Second} }

func TestSupports(t *testing.T) {
	cases := []struct {
		itype  string
		action ActionType
		want   bool
	}{
		{"paloalto", ActionBlockIP, true},
		{"paloalto", ActionUnblockIP, true},
		{"paloalto", ActionContainHost, false}, // firewalls don't contain hosts
		{"fortinet", ActionBlockIP, true},
		{"crowdstrike", ActionContainHost, true},
		{"crowdstrike", ActionLiftContainment, true},
		{"crowdstrike", ActionBlockIP, false}, // EDRs don't block IPs
		{"unknown", ActionBlockIP, false},
	}
	for _, c := range cases {
		if got := Supports(c.itype, c.action); got != c.want {
			t.Errorf("Supports(%q, %q) = %v, want %v", c.itype, c.action, got, c.want)
		}
	}
}

func TestPaloAltoBlock(t *testing.T) {
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		gotForm = string(b)
		_, _ = w.Write([]byte(`<response status="success"><result>OK</result></response>`))
	}))
	defer srv.Close()

	r := &PaloAltoResponder{httpClient: testClient()}
	out, err := r.Execute(context.Background(),
		map[string]string{"paloalto_api_key": "SECRET", "paloalto_base_url": srv.URL},
		Action{Type: ActionBlockIP, Target: "203.0.113.9"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "blocked") || !strings.Contains(out, "203.0.113.9") {
		t.Errorf("unexpected output: %q", out)
	}
	if !strings.Contains(gotForm, "type=user-id") || !strings.Contains(gotForm, "key=SECRET") {
		t.Errorf("form missing type/key: %q", gotForm)
	}
	// cmd is URL-encoded; the register op and IP must be present.
	if !strings.Contains(gotForm, "register") || !strings.Contains(gotForm, "203.0.113.9") {
		t.Errorf("form missing register/IP: %q", gotForm)
	}
	if strings.Contains(gotForm, "unregister") {
		t.Errorf("block must not use unregister: %q", gotForm)
	}
}

func TestPaloAltoUnblock(t *testing.T) {
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotForm = string(b)
		_, _ = w.Write([]byte(`<response status="success"/>`))
	}))
	defer srv.Close()

	r := &PaloAltoResponder{httpClient: testClient()}
	out, err := r.Execute(context.Background(),
		map[string]string{"paloalto_api_key": "SECRET", "paloalto_base_url": srv.URL},
		Action{Type: ActionUnblockIP, Target: "203.0.113.9"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !strings.Contains(out, "unblocked") {
		t.Errorf("unexpected output: %q", out)
	}
	if !strings.Contains(gotForm, "unregister") {
		t.Errorf("unblock must use unregister: %q", gotForm)
	}
}

func TestPaloAltoMissingCred(t *testing.T) {
	r := &PaloAltoResponder{httpClient: testClient()}
	_, err := r.Execute(context.Background(),
		map[string]string{"paloalto_base_url": "https://fw"}, // no api key
		Action{Type: ActionBlockIP, Target: "1.2.3.4"})
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
}

func TestPaloAltoVendorError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<response status="error" code="403"><msg>Invalid Credential</msg></response>`))
	}))
	defer srv.Close()

	r := &PaloAltoResponder{httpClient: testClient()}
	_, err := r.Execute(context.Background(),
		map[string]string{"paloalto_api_key": "BAD", "paloalto_base_url": srv.URL},
		Action{Type: ActionBlockIP, Target: "1.2.3.4"})
	if err == nil {
		t.Fatal("expected error on non-success vendor response")
	}
}

func TestFortinetBlock(t *testing.T) {
	var hitAddress, hitGroup bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer TOK" {
			t.Errorf("missing/incorrect bearer: %q", r.Header.Get("Authorization"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/cmdb/firewall/address"):
			hitAddress = true
		case strings.Contains(r.URL.Path, "/addrgrp/") && strings.HasSuffix(r.URL.Path, "/member"):
			hitGroup = true
		}
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	r := &FortinetResponder{httpClient: testClient()}
	out, err := r.Execute(context.Background(),
		map[string]string{"fortinet_api_token": "TOK", "fortinet_base_url": srv.URL},
		Action{Type: ActionBlockIP, Target: "198.51.100.7"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !hitAddress || !hitGroup {
		t.Errorf("expected both address(%v) and group(%v) calls", hitAddress, hitGroup)
	}
	if !strings.Contains(out, "blocked") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestFortinetGroupFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/addrgrp/") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"status":"error"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	r := &FortinetResponder{httpClient: testClient()}
	_, err := r.Execute(context.Background(),
		map[string]string{"fortinet_api_token": "TOK", "fortinet_base_url": srv.URL},
		Action{Type: ActionBlockIP, Target: "198.51.100.7"})
	if err == nil {
		t.Fatal("expected error when the enforcing group call fails")
	}
}

func TestCrowdStrikeContain(t *testing.T) {
	var tokenHit, actionHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			tokenHit = true
			_, _ = w.Write([]byte(`{"access_token":"abc123","expires_in":1799}`))
		case "/devices/entities/devices-actions/v2":
			actionHit = true
			if r.URL.Query().Get("action_name") != "contain" {
				t.Errorf("expected action_name=contain, got %q", r.URL.Query().Get("action_name"))
			}
			if r.Header.Get("Authorization") != "Bearer abc123" {
				t.Errorf("missing bearer token: %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"resources":[{"id":"dev1"}]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	r := &CrowdStrikeResponder{httpClient: testClient()}
	out, err := r.Execute(context.Background(),
		map[string]string{"crowdstrike_client_id": "cid", "crowdstrike_client_secret": "sec", "crowdstrike_base_url": srv.URL},
		Action{Type: ActionContainHost, Target: "dev1"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !tokenHit || !actionHit {
		t.Errorf("expected token(%v) and action(%v) calls", tokenHit, actionHit)
	}
	if !strings.Contains(out, "contained") || !strings.Contains(out, "dev1") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestCrowdStrikeAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"access denied"}]}`))
	}))
	defer srv.Close()

	r := &CrowdStrikeResponder{httpClient: testClient()}
	_, err := r.Execute(context.Background(),
		map[string]string{"crowdstrike_client_id": "cid", "crowdstrike_client_secret": "bad", "crowdstrike_base_url": srv.URL},
		Action{Type: ActionContainHost, Target: "dev1"})
	if err == nil {
		t.Fatal("expected error on OAuth failure")
	}
}
