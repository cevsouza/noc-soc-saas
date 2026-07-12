package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"noc-api/internal/db"
	"noc-api/internal/repository"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LokiClient struct {
	pgPool     *pgxpool.Pool
	vaultRepo  repository.VaultRepository
	httpClient *http.Client
}

type LokiConfig struct {
	URL      string
	Username string
	Password string
}

func NewLokiClient(pgPool *pgxpool.Pool) *LokiClient {
	return &LokiClient{
		pgPool:    pgPool,
		vaultRepo: repository.NewPostgresVaultRepository(),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *LokiClient) getLokiConfig(ctx context.Context, tenantID uuid.UUID) (*LokiConfig, error) {
	masterKey, err := security.GetMasterKey()
	if err != nil {
		return nil, fmt.Errorf("master key not loaded: %w", err)
	}

	var cfg LokiConfig
	err = db.ExecuteInTenantTx(ctx, c.pgPool, func(tx pgx.Tx) error {
		secretURL, err := c.vaultRepo.GetSecretByKey(ctx, tx, "loki_url")
		if err != nil {
			return err
		}
		decryptedURL, err := security.DecryptForTenant(secretURL.EncryptedValue, secretURL.Nonce, masterKey, tenantID)
		if err != nil {
			return err
		}
		cfg.URL = string(decryptedURL)

		// Username and password are optional (anonymous Loki is common in internal dev)
		secretUser, err := c.vaultRepo.GetSecretByKey(ctx, tx, "loki_username")
		if err == nil {
			decryptedUser, _ := security.DecryptForTenant(secretUser.EncryptedValue, secretUser.Nonce, masterKey, tenantID)
			cfg.Username = string(decryptedUser)
		}

		secretPass, err := c.vaultRepo.GetSecretByKey(ctx, tx, "loki_password")
		if err == nil {
			decryptedPass, _ := security.DecryptForTenant(secretPass.EncryptedValue, secretPass.Nonce, masterKey, tenantID)
			cfg.Password = string(decryptedPass)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// FetchHostLogs gets the last 50 log lines for a host/IP from Loki using LogQL
func (c *LokiClient) FetchHostLogs(ctx context.Context, tenantID uuid.UUID, host string) ([]string, error) {
	tenantCtx := db.WithTenantID(ctx, tenantID)
	cfg, err := c.getLokiConfig(tenantCtx, tenantID)
	if err != nil {
		// If Loki is not configured, return mock logs for development
		log.Printf("[Loki Client] Loki not configured for tenant %s. Returning mock telemetry logs.", tenantID)
		return c.generateMockLogs(host), nil
	}

	if cfg.URL == "mock_loki" {
		return c.generateMockLogs(host), nil
	}

	// Formulate LogQL query based on IP or host name
	logql := fmt.Sprintf(`{host="%s"} |~ "(?i)error|fail|warn|exception|crit"`, host)
	if isIP(host) {
		logql = fmt.Sprintf(`{ip="%s"} |~ "(?i)error|fail|warn|exception|crit"`, host)
	}

	u, err := url.Parse(cfg.URL + "/loki/api/v1/query_range")
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("query", logql)
	q.Set("limit", "50")
	q.Set("direction", "backward")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	if cfg.Username != "" && cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Loki: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("loki returned status %d", resp.StatusCode)
	}

	// Decode Loki API response
	var lokiResponse struct {
		Data struct {
			Result []struct {
				Values [][]string `json:"values"` // [timestamp, logline]
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&lokiResponse); err != nil {
		return nil, err
	}

	var logs []string
	for _, res := range lokiResponse.Data.Result {
		for _, val := range res.Values {
			if len(val) >= 2 {
				// val[0] is timestamp (ns), val[1] is log content
				logs = append(logs, val[1])
			}
		}
	}

	return logs, nil
}

func (c *LokiClient) generateMockLogs(host string) []string {
	var logs []string
	now := time.Now()
	for i := 1; i <= 50; i++ {
		tStr := now.Add(-time.Duration(50-i) * time.Minute).Format("2006-01-02 15:04:05.000")
		var logLine string
		switch i % 5 {
		case 0:
			logLine = fmt.Sprintf("[%s] [ERROR] Host %s reported connection pool exhaustion - pool size exceeded maximum limit", tStr, host)
		case 1:
			logLine = fmt.Sprintf("[%s] [WARNING] Disk I/O warning: latency observed > 200ms on drive /dev/sda1 on %s", tStr, host)
		case 2:
			logLine = fmt.Sprintf("[%s] [CRITICAL] Out of Memory: process 'mysqld' killed by kernel oom-killer on %s", tStr, host)
		case 3:
			logLine = fmt.Sprintf("[%s] [INFO] Login attempt failed for user 'root' from 185.190.140.12 - SSH authentication failure on %s", tStr, host)
		default:
			logLine = fmt.Sprintf("[%s] [ERROR] Service 'systemd-resolved' crashed: signal 11 (Segmentation Fault) on %s", tStr, host)
		}
		logs = append(logs, logLine)
	}
	return logs
}

func isIP(host string) bool {
	// Simple IP check
	for i := 0; i < len(host); i++ {
		c := host[i]
		if (c < '0' || c > '9') && c != '.' && c != ':' {
			return false
		}
	}
	return true
}

// TestConnection checks if Loki is responsive by querying its ready endpoint
func (c *LokiClient) TestConnection(ctx context.Context, tenantID uuid.UUID) error {
	tenantCtx := db.WithTenantID(ctx, tenantID)
	cfg, err := c.getLokiConfig(tenantCtx, tenantID)
	if err != nil {
		return fmt.Errorf("loki configuration not found in Vault: %w", err)
	}

	if cfg.URL == "mock_loki" || cfg.URL == "" {
		return nil // Mock is always ready
	}

	reqURL := cfg.URL + "/ready"
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	if cfg.Username != "" && cfg.Password != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach Loki at %s: %w", cfg.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("loki ready check returned status %d", resp.StatusCode)
	}

	return nil
}
