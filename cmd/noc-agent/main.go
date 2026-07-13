// Command noc-agent is the NOC/SOC agent that runs on a customer's network and talks to the SaaS ONLY
// outbound over HTTPS (443): it enrolls once (token -> API key), polls its config, and pushes
// heartbeats/events. No inbound firewall rules are ever needed.
//
// Slice 1 establishes this outbound channel and liveness. The SNMP collector (slice 2) will fill the
// event batch with real network-device metrics/threshold alerts.
//
// Usage (first run enrolls, then reuses the stored key):
//
//	NOC_SAAS_URL=https://your-saas NOC_ENROLLMENT_TOKEN=<token> noc-agent
//	NOC_SAAS_URL=https://your-saas noc-agent   # subsequent runs use noc-agent-state.json
package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"noc-api/internal/agent"
)

const agentVersion = "0.1.0"

type state struct {
	AgentID string `json:"agent_id"`
	APIKey  string `json:"api_key"`
}

func loadState(path string) (state, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return state{}, false
	}
	var s state
	if err := json.Unmarshal(b, &s); err != nil || s.APIKey == "" {
		return state{}, false
	}
	return s, true
}

func saveState(path string, s state) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	saasURL := env("NOC_SAAS_URL", "")
	if saasURL == "" {
		log.Fatal("NOC_SAAS_URL is required")
	}
	statePath := env("NOC_AGENT_STATE", "noc-agent-state.json")
	hostname, _ := os.Hostname()
	hostname = env("NOC_AGENT_HOSTNAME", hostname)

	client := agent.New(saasURL)

	st, ok := loadState(statePath)
	if ok {
		client.APIKey = st.APIKey
		log.Printf("[agent] loaded stored credentials (agent %s)", st.AgentID)
	} else {
		token := env("NOC_ENROLLMENT_TOKEN", "")
		if token == "" {
			log.Fatal("no stored credentials and NOC_ENROLLMENT_TOKEN not set — cannot enroll")
		}
		agentID, err := client.Enroll(token, hostname, agentVersion)
		if err != nil {
			log.Fatalf("[agent] enrollment failed: %v", err)
		}
		st = state{AgentID: agentID, APIKey: client.APIKey}
		if err := saveState(statePath, st); err != nil {
			log.Fatalf("[agent] failed to persist credentials: %v", err)
		}
		log.Printf("[agent] enrolled as agent %s (credentials saved to %s)", agentID, statePath)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	pollInterval := 60 * time.Second
	poller := agent.NewPoller()
	log.Printf("[agent] noc-agent %s started; polling %s every %s (outbound 443 only)", agentVersion, saasURL, pollInterval)

	runCycle := func() {
		cfg, err := client.GetConfig(st.AgentID)
		if err != nil {
			log.Printf("[agent] config poll failed: %v", err)
			return
		}
		if cfg.PollIntervalSeconds > 0 {
			pollInterval = time.Duration(cfg.PollIntervalSeconds) * time.Second
		}
		// Collect SNMP threshold breaches from the configured targets. An empty batch is a heartbeat.
		events := agent.Collect(poller, cfg.SNMPTargets)
		accepted, err := client.SendEvents(st.AgentID, events)
		if err != nil {
			log.Printf("[agent] events push failed: %v", err)
			return
		}
		log.Printf("[agent] cycle ok (snmp_targets=%d, events=%d accepted=%d, next in %s)", len(cfg.SNMPTargets), len(events), accepted, pollInterval)
	}

	runCycle()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			log.Println("[agent] shutting down")
			return
		case <-ticker.C:
			runCycle()
			ticker.Reset(pollInterval)
		}
	}
}
