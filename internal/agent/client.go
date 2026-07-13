// Package agent is the NOC/SOC agent's client for the SaaS. It talks to the SaaS ONLY outbound over
// HTTPS (443): enroll once (token -> API key), poll config, push heartbeats/events. It intentionally
// duplicates the small wire structs (rather than importing internal/api) so the agent stays a
// self-contained, minimal deployable.
package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config is what the agent polls to learn what to do.
type Config struct {
	HeartbeatIntervalSeconds int          `json:"heartbeat_interval_seconds"`
	PollIntervalSeconds      int          `json:"poll_interval_seconds"`
	SNMPTargets              []SNMPTarget `json:"snmp_targets"`
}

// Event is one event the agent pushes (e.g. an SNMP threshold breach in slice 2).
type Event struct {
	Source      string `json:"source"`
	ExternalID  string `json:"external_id"`
	EventType   string `json:"event_type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Host        string `json:"host"`
}

// Client is the agent's SaaS client. APIKey is empty until enrollment completes.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New builds a client for the given SaaS base URL (e.g. https://noc-soc-saas-production.up.railway.app).
func New(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) postJSON(path string, body interface{}, withKey bool) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if withKey {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	return c.HTTP.Do(req)
}

// Enroll exchanges a one-time enrollment token for a tenant API key, storing it on the client.
// Returns the agent id assigned by the SaaS.
func (c *Client) Enroll(token, hostname, version string) (agentID string, err error) {
	resp, err := c.postJSON("/api/v1/agent/enroll", map[string]string{
		"enrollment_token": token, "hostname": hostname, "version": version,
	}, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("enroll failed: status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		AgentID string `json:"agent_id"`
		APIKey  string `json:"api_key"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", err
	}
	if out.APIKey == "" {
		return "", fmt.Errorf("enroll response missing api_key")
	}
	c.APIKey = out.APIKey
	return out.AgentID, nil
}

// GetConfig polls the agent's configuration (requires APIKey set).
func (c *Client) GetConfig(agentID string) (Config, error) {
	var cfg Config
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/api/v1/agent/config?agent_id="+agentID, nil)
	if err != nil {
		return cfg, err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return cfg, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return cfg, fmt.Errorf("config failed: status %d: %s", resp.StatusCode, string(body))
	}
	return cfg, json.Unmarshal(body, &cfg)
}

// SendEvents pushes a batch of events (an empty batch is a valid heartbeat-only ping). Returns the
// number accepted by the SaaS.
func (c *Client) SendEvents(agentID string, events []Event) (int, error) {
	resp, err := c.postJSON("/api/v1/agent/events?agent_id="+agentID, map[string]interface{}{
		"agent_id": agentID, "events": events,
	}, true)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		return 0, fmt.Errorf("events failed: status %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		Accepted int `json:"accepted"`
	}
	_ = json.Unmarshal(body, &out)
	return out.Accepted, nil
}
