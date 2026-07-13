package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentClientFlow(t *testing.T) {
	const wantKey = "test-api-key-123"
	const wantAgent = "agent-abc"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/enroll", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["enrollment_token"] != "tok" || body["hostname"] == "" {
			t.Errorf("enroll got unexpected body: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"agent_id": wantAgent, "api_key": wantKey})
	})
	mux.HandleFunc("/api/v1/agent/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("agent_id") != wantAgent {
			t.Errorf("config missing agent_id")
		}
		_ = json.NewEncoder(w).Encode(Config{HeartbeatIntervalSeconds: 60, PollIntervalSeconds: 45, SNMPTargets: nil})
	})
	mux.HandleFunc("/api/v1/agent/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			AgentID string  `json:"agent_id"`
			Events  []Event `json:"events"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(body.Events)})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.URL)

	// Enroll sets the API key and returns the agent id.
	agentID, err := c.Enroll("tok", "host1", "0.1.0")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if agentID != wantAgent || c.APIKey != wantKey {
		t.Fatalf("enroll result agentID=%q apiKey=%q", agentID, c.APIKey)
	}

	// Config poll honours the API key + poll interval.
	cfg, err := c.GetConfig(agentID)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.PollIntervalSeconds != 45 {
		t.Fatalf("poll interval = %d, want 45", cfg.PollIntervalSeconds)
	}

	// Empty batch = heartbeat (0 accepted).
	if n, err := c.SendEvents(agentID, nil); err != nil || n != 0 {
		t.Fatalf("heartbeat: n=%d err=%v", n, err)
	}
	// A real batch is accepted.
	n, err := c.SendEvents(agentID, []Event{{Source: "snmp", EventType: "iface_down", Severity: "critical", Title: "port down"}})
	if err != nil || n != 1 {
		t.Fatalf("send events: n=%d err=%v", n, err)
	}
}

func TestEnrollRejectsMissingKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"agent_id": "x"}) // no api_key
	}))
	defer srv.Close()
	if _, err := New(srv.URL).Enroll("tok", "h", "v"); err == nil {
		t.Fatal("expected error when enroll response omits api_key")
	}
}
