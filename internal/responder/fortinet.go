package responder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FortinetResponder blocks/unblocks a source IP on a FortiGate (FortiOS) firewall via the REST
// config API: it creates a firewall address object for the IP and adds it to a blocklist address
// group (default "noc-blocklist") that the admin references from a deny policy. Unblock reverses
// both. The block is enforced by the group membership, so that step is the one that hard-fails;
// the address-object create/delete is best-effort and only annotated in the output.
//
// Credentials (per-tenant vault keys):
//   - fortinet_api_token   (required) — FortiOS REST API token (sent as Bearer)
//   - fortinet_base_url    (required) — e.g. https://fw.example.com
//   - fortinet_block_group (optional) — address group name; defaults to "noc-blocklist"
type FortinetResponder struct {
	httpClient *http.Client
}

func init() { Register(&FortinetResponder{httpClient: &http.Client{Timeout: 10 * time.Second}}) }

func (r *FortinetResponder) IntegrationType() string { return "fortinet" }

func (r *FortinetResponder) SupportedActions() []ActionType {
	return []ActionType{ActionBlockIP, ActionUnblockIP}
}

func (r *FortinetResponder) Execute(ctx context.Context, creds map[string]string, action Action) (string, error) {
	token, err := requireCred(creds, "fortinet_api_token")
	if err != nil {
		return "", err
	}
	baseURL, err := requireCred(creds, "fortinet_base_url")
	if err != nil {
		return "", err
	}
	group := credOrDefault(creds, "fortinet_block_group", "noc-blocklist")
	base := strings.TrimRight(baseURL, "/")

	if action.Target == "" {
		return "", fmt.Errorf("block/unblock requires a target IP address")
	}
	addrName := "noc_block_" + strings.NewReplacer(".", "_", ":", "_", "/", "_").Replace(action.Target)

	switch action.Type {
	case ActionBlockIP:
		// 1. Create the address object (best-effort: tolerate "already exists").
		addrNote := ""
		if _, err := r.do(ctx, token, http.MethodPost, base+"/api/v2/cmdb/firewall/address",
			map[string]interface{}{"name": addrName, "type": "ipmask", "subnet": action.Target + "/32"}); err != nil {
			addrNote = fmt.Sprintf(" (address-object note: %v)", err)
		}
		// 2. Add it to the blocklist group — this is the enforcing step.
		if _, err := r.do(ctx, token, http.MethodPost,
			base+"/api/v2/cmdb/firewall/addrgrp/"+group+"/member",
			map[string]interface{}{"name": addrName}); err != nil {
			return "", fmt.Errorf("failed to add %s to FortiGate group %q: %w", action.Target, group, err)
		}
		return fmt.Sprintf("Fortinet: blocked IP %s via address group %q%s", action.Target, group, addrNote), nil

	case ActionUnblockIP:
		// 1. Remove from the group — the enforcing step.
		if _, err := r.do(ctx, token, http.MethodDelete,
			base+"/api/v2/cmdb/firewall/addrgrp/"+group+"/member/"+addrName, nil); err != nil {
			return "", fmt.Errorf("failed to remove %s from FortiGate group %q: %w", action.Target, group, err)
		}
		// 2. Delete the address object (best-effort).
		addrNote := ""
		if _, err := r.do(ctx, token, http.MethodDelete, base+"/api/v2/cmdb/firewall/address/"+addrName, nil); err != nil {
			addrNote = fmt.Sprintf(" (address-object note: %v)", err)
		}
		return fmt.Sprintf("Fortinet: unblocked IP %s from address group %q%s", action.Target, group, addrNote), nil

	default:
		return "", fmt.Errorf("fortinet responder does not support action %q", action.Type)
	}
}

// do issues one FortiOS REST call and validates the response. FortiOS returns 200 with a JSON
// body carrying "status":"success" on success; a duplicate-object create returns error code -5,
// which the caller treats as tolerable for idempotency.
func (r *FortinetResponder) do(ctx context.Context, token, method, endpoint string, body map[string]interface{}) (string, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return "", fmt.Errorf("failed to build FortiOS request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("FortiOS API call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	text := string(respBody)

	// Tolerate "already exists" (error -5) so re-blocking an IP is idempotent.
	if strings.Contains(text, "-5") && strings.Contains(strings.ToLower(text), "duplicate") {
		return text, nil
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(text, `"status":"success"`) {
		return "", fmt.Errorf("FortiOS API returned status %d: %s", resp.StatusCode, strings.TrimSpace(text))
	}
	return text, nil
}
