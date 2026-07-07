package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/db"
	"noc-api/internal/model"
	"noc-api/internal/repository"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type SentinelConfig struct {
	TenantID       string
	ClientID       string
	ClientSecret   string
	SubscriptionID string
	ResourceGroup  string
	WorkspaceName  string
}

type SentinelConnector struct {
	pgPool      *pgxpool.Pool
	redisClient *redis.Client
	vaultRepo   repository.VaultRepository
	alertRepo   repository.AlertRepository
	httpClient  *http.Client
	mu          sync.Mutex
	running     bool
	stopChan    chan struct{}
}

func NewSentinelConnector(pgPool *pgxpool.Pool, redisClient *redis.Client) *SentinelConnector {
	return &SentinelConnector{
		pgPool:      pgPool,
		redisClient: redisClient,
		vaultRepo:   repository.NewPostgresVaultRepository(),
		alertRepo:   repository.NewPostgresAlertRepository(),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopChan: make(chan struct{}),
	}
}

func (c *SentinelConnector) Start(ctx context.Context, interval time.Duration) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopChan = make(chan struct{})
	c.mu.Unlock()

	log.Printf("Starting Microsoft Sentinel Bidirectional Polling Connector (Interval: %v)...", interval)
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-c.stopChan:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.runSyncCycle(ctx)
			}
		}
	}()
}

func (c *SentinelConnector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return
	}
	close(c.stopChan)
	c.running = false
	log.Println("Microsoft Sentinel Connector stopped.")
}

func (c *SentinelConnector) runSyncCycle(ctx context.Context) {
	// 1. Fetch all active tenants
	tenants, err := c.getActiveTenants(ctx)
	if err != nil {
		log.Printf("[Sentinel Connector] Failed to query active tenants: %v", err)
		return
	}

	masterKey, err := security.GetMasterKey()
	if err != nil {
		log.Printf("[Sentinel Connector] Warning: VAULT_MASTER_KEY not set. Cannot run decryptions. Skipping sync cycle.")
		return
	}

	for _, tenant := range tenants {
		tenantCtx := db.WithTenantID(ctx, tenant.ID)
		cfg, err := c.getSentinelConfig(tenantCtx, tenant.ID, masterKey)
		if err != nil {
			// Tenant does not have Sentinel configured, skip silently
			continue
		}

		// Run sync logic for this tenant
		go func(t model.Tenant, sCfg *SentinelConfig) {
			if sCfg.ClientID == "mock_sentinel" {
				c.runMockSync(tenantCtx, t, sCfg)
				return
			}
			c.syncTenantSentinel(tenantCtx, t, sCfg)
		}(tenant, cfg)
	}
}

func (c *SentinelConnector) getActiveTenants(ctx context.Context) ([]model.Tenant, error) {
	query := "SELECT id, name, slug, status FROM tenants WHERE status = 'active'"
	rows, err := c.pgPool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.Tenant
	for rows.Next() {
		var t model.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Status); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, nil
}

func (c *SentinelConnector) getSentinelConfig(ctx context.Context, tenantID uuid.UUID, masterKey []byte) (*SentinelConfig, error) {
	// We run queries under a tenant transaction to enforce RLS
	var cfg SentinelConfig
	err := db.ExecuteInTenantTx(ctx, c.pgPool, func(tx pgx.Tx) error {
		keys := []string{
			"sentinel_tenant_id",
			"sentinel_client_id",
			"sentinel_client_secret",
			"sentinel_subscription_id",
			"sentinel_resource_group",
			"sentinel_workspace_name",
		}

		vals := make(map[string]string)
		for _, k := range keys {
			secret, err := c.vaultRepo.GetSecretByKey(ctx, tx, k)
			if err != nil {
				return err
			}
			decrypted, err := security.Decrypt(secret.EncryptedValue, secret.Nonce, masterKey)
			if err != nil {
				return err
			}
			vals[k] = string(decrypted)
		}

		cfg = SentinelConfig{
			TenantID:       vals["sentinel_tenant_id"],
			ClientID:       vals["sentinel_client_id"],
			ClientSecret:   vals["sentinel_client_secret"],
			SubscriptionID: vals["sentinel_subscription_id"],
			ResourceGroup:  vals["sentinel_resource_group"],
			WorkspaceName:  vals["sentinel_workspace_name"],
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// syncTenantSentinel pulls incidents from Azure and pushes local state updates back to Azure
func (c *SentinelConnector) syncTenantSentinel(ctx context.Context, tenant model.Tenant, cfg *SentinelConfig) {
	log.Printf("[Sentinel Connector] Syncing tenant '%s' (%s) with Azure Sentinel...", tenant.Name, tenant.ID)

	token, err := c.getAzureAccessToken(ctx, cfg)
	if err != nil {
		errMsg := fmt.Sprintf("Azure OAuth failed: %v", err)
		c.redisClient.Set(ctx, fmt.Sprintf("webhook:error:%s:%s", tenant.ID.String(), "sentinel"), errMsg, 24*time.Hour)
		log.Printf("[Sentinel Connector ERROR] Tenant %s failed Azure OAuth: %v", tenant.ID, err)
		return
	}

	// 1. Pull Incidents from Azure Sentinel
	// Querying incidents updated in the last 15 minutes to capture any updates
	filterTime := time.Now().Add(-15 * time.Minute).UTC().Format(time.RFC3339)
	urlPath := fmt.Sprintf(
		"https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/Microsoft.OperationalInsights/workspaces/%s/providers/Microsoft.SecurityInsights/incidents?api-version=2023-02-01&$filter=properties/lastModifiedTimeUtc ge %s",
		cfg.SubscriptionID, cfg.ResourceGroup, cfg.WorkspaceName, filterTime,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", urlPath, nil)
	if err != nil {
		errMsg := fmt.Sprintf("HTTP NewRequest failed: %v", err)
		c.redisClient.Set(ctx, fmt.Sprintf("webhook:error:%s:%s", tenant.ID.String(), "sentinel"), errMsg, 24*time.Hour)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		errMsg := fmt.Sprintf("Azure API call failed: %v", err)
		c.redisClient.Set(ctx, fmt.Sprintf("webhook:error:%s:%s", tenant.ID.String(), "sentinel"), errMsg, 24*time.Hour)
		log.Printf("[Sentinel Connector ERROR] API call failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Azure Sentinel API returned status %d", resp.StatusCode)
		c.redisClient.Set(ctx, fmt.Sprintf("webhook:error:%s:%s", tenant.ID.String(), "sentinel"), errMsg, 24*time.Hour)
		log.Printf("[Sentinel Connector ERROR] Azure Sentinel API returned status %d", resp.StatusCode)
		return
	}

	var result struct {
		Value []struct {
			Name       string `json:"name"` // External ID
			Properties struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				Severity    string `json:"severity"` // High, Medium, Low, Informational
				Status      string `json:"status"`   // New, Active, Closed
				CreatedTime string `json:"createdTimeUtc"`
			} `json:"properties"`
		} `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}

	// Loop and push to Redis Queue
	for _, rawInc := range result.Value {
		severity := model.SeverityInfo
		switch strings.ToLower(rawInc.Properties.Severity) {
		case "high":
			severity = model.SeverityCritical
		case "medium":
			severity = model.SeverityWarning
		case "low":
			severity = model.SeverityInfo
		}

		timestamp, err := time.Parse(time.RFC3339, rawInc.Properties.CreatedTime)
		if err != nil {
			timestamp = time.Now()
		}

		incident := model.UnifiedIncident{
			ID:          uuid.New(),
			TenantID:    tenant.ID,
			Source:      model.SourceSentinel,
			ExternalID:  rawInc.Name,
			EventType:   "sentinel_security_incident",
			Severity:    severity,
			Title:       rawInc.Properties.Title,
			Description: rawInc.Properties.Description,
			Host:        "azure_cloud",
			RawPayload: map[string]interface{}{
				"azure_status": rawInc.Properties.Status,
			},
			Timestamp: timestamp,
		}

		bytes, _ := json.Marshal(incident)
		_ = c.redisClient.LPush(ctx, api.AlertsQueueKey, bytes).Err()
	}

	// Register heartbeat in Redis and clear any previous errors
	c.redisClient.Set(ctx, fmt.Sprintf("heartbeat:connector:%s:%s", tenant.ID.String(), "sentinel"), time.Now().Unix(), 24*time.Hour)
	c.redisClient.Del(ctx, fmt.Sprintf("webhook:error:%s:%s", tenant.ID.String(), "sentinel"))
}

func (c *SentinelConnector) getAzureAccessToken(ctx context.Context, cfg *SentinelConfig) (string, error) {
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID)

	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", cfg.ClientID)
	data.Set("client_secret", cfg.ClientSecret)
	data.Set("scope", "https://management.azure.com/.default")

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OAuth server returned status %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

func (c *SentinelConnector) runMockSync(ctx context.Context, tenant model.Tenant, cfg *SentinelConfig) {
	log.Printf("[Sentinel Mock Connector] Syncing mock Sentinel for tenant '%s'...", tenant.Name)

	// Simulate receiving a new incident from Sentinel
	incidentID := fmt.Sprintf("sentinel-mock-%d", time.Now().Unix())
	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenant.ID,
		Source:      model.SourceSentinel,
		ExternalID:  incidentID,
		EventType:   "sentinel_security_incident",
		Severity:    model.SeverityCritical,
		Title:       "[Sentinel SOC Alert] Unauthorized administrative privilege escalation detected",
		Description: "Audit logs indicated administrative privilege escalation on system 'ad-domain-controller-01'. Severity High.",
		Host:        "ad-domain-controller-01",
		RawPayload: map[string]interface{}{
			"azure_status": "New",
			"resource":     "AD-Domain-Controller-01",
		},
		Timestamp: time.Now(),
	}

	bytes, _ := json.Marshal(incident)
	_ = c.redisClient.LPush(ctx, api.AlertsQueueKey, bytes).Err()
	log.Printf("[Sentinel Mock Connector] Mock incident pushed to queue: %s", incidentID)

	// Register heartbeat in Redis and clear any previous errors
	c.redisClient.Set(ctx, fmt.Sprintf("heartbeat:connector:%s:%s", tenant.ID.String(), "sentinel"), time.Now().Unix(), 24*time.Hour)
	c.redisClient.Del(ctx, fmt.Sprintf("webhook:error:%s:%s", tenant.ID.String(), "sentinel"))
}
