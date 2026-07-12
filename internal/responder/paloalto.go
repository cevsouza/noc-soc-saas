package responder

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PaloAltoResponder blocks/unblocks a source IP on a PAN-OS firewall by (un)registering it to a
// Dynamic Address Group (DAG) tag via the User-ID XML API. The admin builds a Security policy
// rule that denies traffic from a DAG matching the block tag (default "noc-blocklist"), so
// tagging an IP here immediately drops it without a commit.
//
// Credentials (per-tenant vault keys):
//   - paloalto_api_key   (required)  — PAN-OS API key
//   - paloalto_base_url  (required)  — e.g. https://fw.example.com  (management URL, no trailing /api)
//   - paloalto_block_tag (optional)  — DAG tag to register into; defaults to "noc-blocklist"
type PaloAltoResponder struct {
	httpClient *http.Client
}

func init() { Register(&PaloAltoResponder{httpClient: &http.Client{Timeout: 10 * time.Second}}) }

func (r *PaloAltoResponder) IntegrationType() string { return "paloalto" }

func (r *PaloAltoResponder) SupportedActions() []ActionType {
	return []ActionType{ActionBlockIP, ActionUnblockIP}
}

func (r *PaloAltoResponder) Execute(ctx context.Context, creds map[string]string, action Action) (string, error) {
	apiKey, err := requireCred(creds, "paloalto_api_key")
	if err != nil {
		return "", err
	}
	baseURL, err := requireCred(creds, "paloalto_base_url")
	if err != nil {
		return "", err
	}
	tag := credOrDefault(creds, "paloalto_block_tag", "noc-blocklist")

	if action.Target == "" {
		return "", fmt.Errorf("block/unblock requires a target IP address")
	}

	var op string // register (block) or unregister (unblock)
	switch action.Type {
	case ActionBlockIP:
		op = "register"
	case ActionUnblockIP:
		op = "unregister"
	default:
		return "", fmt.Errorf("paloalto responder does not support action %q", action.Type)
	}

	// PAN-OS User-ID uid-message. XML-escape the caller-supplied IP so a malformed target can't
	// break out of the attribute (defense in depth — the handler already validates it's an IP).
	cmd := fmt.Sprintf(
		`<uid-message><version>2.0</version><type>update</type><payload><%s><entry ip=%q><tag><member>%s</member></tag></entry></%s></payload></uid-message>`,
		op, action.Target, xmlEscape(tag), op,
	)

	form := url.Values{}
	form.Set("type", "user-id")
	form.Set("key", apiKey)
	form.Set("cmd", cmd)

	endpoint := strings.TrimRight(baseURL, "/") + "/api/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to build PAN-OS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("PAN-OS API call failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `status="success"`) {
		return "", fmt.Errorf("PAN-OS API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	verb := "blocked"
	if action.Type == ActionUnblockIP {
		verb = "unblocked"
	}
	return fmt.Sprintf("Palo Alto: %s IP %s via DAG tag %q", verb, action.Target, tag), nil
}

// xmlEscape escapes the handful of characters that matter inside XML text/attribute content.
func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}
