package responder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CrowdStrikeResponder contains (network-isolates) or lifts containment on a Falcon-managed host
// via the Falcon Hosts API. It first exchanges the OAuth2 client credentials for a bearer token,
// then calls the devices-actions endpoint with the target device id.
//
// Credentials (per-tenant vault keys):
//   - crowdstrike_client_id     (required) — Falcon OAuth2 client id
//   - crowdstrike_client_secret (required) — Falcon OAuth2 client secret
//   - crowdstrike_base_url      (optional) — API region base; defaults to https://api.crowdstrike.com
type CrowdStrikeResponder struct {
	httpClient *http.Client
}

func init() { Register(&CrowdStrikeResponder{httpClient: &http.Client{Timeout: 10 * time.Second}}) }

func (r *CrowdStrikeResponder) IntegrationType() string { return "crowdstrike" }

func (r *CrowdStrikeResponder) SupportedActions() []ActionType {
	return []ActionType{ActionContainHost, ActionLiftContainment}
}

func (r *CrowdStrikeResponder) Execute(ctx context.Context, creds map[string]string, action Action) (string, error) {
	clientID, err := requireCred(creds, "crowdstrike_client_id")
	if err != nil {
		return "", err
	}
	clientSecret, err := requireCred(creds, "crowdstrike_client_secret")
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(credOrDefault(creds, "crowdstrike_base_url", "https://api.crowdstrike.com"), "/")

	if action.Target == "" {
		return "", fmt.Errorf("contain/lift requires a target device id")
	}

	var actionName string
	switch action.Type {
	case ActionContainHost:
		actionName = "contain"
	case ActionLiftContainment:
		actionName = "lift_containment"
	default:
		return "", fmt.Errorf("crowdstrike responder does not support action %q", action.Type)
	}

	token, err := r.authenticate(ctx, base, clientID, clientSecret)
	if err != nil {
		return "", err
	}

	reqBody, _ := json.Marshal(map[string]interface{}{"ids": []string{action.Target}})
	endpoint := base + "/devices/entities/devices-actions/v2?action_name=" + url.QueryEscape(actionName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to build Falcon action request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Falcon action call failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	// Falcon returns 202 Accepted on a queued action, 200 on immediate success.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("Falcon action returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	verb := "contained"
	if action.Type == ActionLiftContainment {
		verb = "lifted containment on"
	}
	return fmt.Sprintf("CrowdStrike: %s host %s", verb, action.Target), nil
}

// authenticate exchanges the OAuth2 client credentials for a short-lived bearer token.
func (r *CrowdStrikeResponder) authenticate(ctx context.Context, base, clientID, clientSecret string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build Falcon token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Falcon token call failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("Falcon token endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse Falcon token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("Falcon token response contained no access_token")
	}
	return tokenResp.AccessToken, nil
}
