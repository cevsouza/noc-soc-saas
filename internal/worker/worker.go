package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/db"
	"noc-api/internal/executor"
	"noc-api/internal/loki"
	"noc-api/internal/model"
	"noc-api/internal/repository"

	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	DebounceKeyPrefix = "noc:debounce:"
	DebounceTTL       = 5 * time.Minute
	PubSubChannel     = "noc:pubsub:tenant:"
)

type WorkerPool struct {
	pgPool      *pgxpool.Pool
	redisClient *redis.Client
	alertRepo   repository.AlertRepository
	numWorkers  int
	wg          sync.WaitGroup
	stopChan    chan struct{}
	executor    *executor.SelfHealingExecutor
	lokiClient  *loki.LokiClient
}

func NewWorkerPool(pgPool *pgxpool.Pool, redisClient *redis.Client, numWorkers int) *WorkerPool {
	return &WorkerPool{
		pgPool:      pgPool,
		redisClient: redisClient,
		alertRepo:   repository.NewPostgresAlertRepository(),
		numWorkers:  numWorkers,
		stopChan:    make(chan struct{}),
		executor:    executor.NewSelfHealingExecutor(pgPool),
		lokiClient:  loki.NewLokiClient(pgPool),
	}
}

func (wp *WorkerPool) Start(ctx context.Context) {
	log.Printf("Starting %d concurrent background alert workers...", wp.numWorkers)
	for i := 0; i < wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(ctx, i)
	}
}

func (wp *WorkerPool) Stop() {
	log.Println("Stopping alert workers...")
	close(wp.stopChan)
	wp.wg.Wait()
	log.Println("All background workers stopped gracefully.")
}

func (wp *WorkerPool) worker(ctx context.Context, id int) {
	defer wp.wg.Done()
	log.Printf("Worker %d started and waiting for events...", id)

	for {
		select {
		case <-wp.stopChan:
			return
		case <-ctx.Done():
			return
		default:
			// BRPOP: Blocking POP from the right (LPUSH put it on the left)
			// Timeout is set to 2 seconds to allow regular checking of stopChan
			result, err := wp.redisClient.BRPop(ctx, 2*time.Second, api.AlertsNormalizedQueueKey).Result()
			if err != nil {
				if err == redis.Nil {
					// Queue empty, continue polling
					continue
				}
				log.Printf("[Worker %d] Error popping from queue: %v", id, err)
				time.Sleep(1 * time.Second) // Prevent tight crash loop
				continue
			}

			// BRPOP returns a slice: [key_name, element_value]
			if len(result) < 2 {
				continue
			}
			eventBytes := []byte(result[1])

			var incident model.UnifiedIncident
			if err := json.Unmarshal(eventBytes, &incident); err != nil {
				log.Printf("[Worker %d] Error unmarshalling queued event: %v", id, err)
				continue
			}

			// Process the popped event
			if err := wp.processEvent(ctx, incident); err != nil {
				log.Printf("[Worker %d] Error processing event %s: %v", id, incident.ID, err)
			}
		}
	}
}

func (wp *WorkerPool) processEvent(ctx context.Context, event model.UnifiedIncident) error {
	// Construct RLS context for this tenant
	tenantCtx := db.WithTenantID(ctx, event.TenantID)

	// 0. Dynamic Ingestion Normalization: lookup mapping rules in PostgreSQL
	var severityOverride string
	queryRule := `
		SELECT normalized_value 
		FROM tenant_mapping_rules 
		WHERE tenant_id = $1 AND source_tool = $2 AND source_field = 'severity' AND source_value = $3
	`
	err := wp.pgPool.QueryRow(tenantCtx, queryRule, event.TenantID, string(event.Source), string(event.Severity)).Scan(&severityOverride)
	if err == nil && severityOverride != "" {
		event.Severity = model.AlertSeverity(severityOverride)
	}

	var deviceIDStr string
	if event.DeviceID != nil {
		deviceIDStr = event.DeviceID.String()
	} else {
		deviceIDStr = "nil_device"
	}

	// 1. Debounce checking key signature: noc:debounce:<tenant_id>:<device_id>:<event_type>
	debounceKey := fmt.Sprintf("%s%s:%s:%s", DebounceKeyPrefix, event.TenantID, deviceIDStr, event.EventType)

	// Try reading debounce pointer from Redis
	existingAlertIDStr, err := wp.redisClient.Get(ctx, debounceKey).Result()
	if err == nil && existingAlertIDStr != "" {
		// DEBOUNCE HIT: An active alert of this exact type exists in the last 5 minutes.
		existingAlertID, parseErr := uuid.Parse(existingAlertIDStr)
		if parseErr == nil {
			log.Printf("[Debounce Engine] Hit! Agrouping event type '%s' for tenant %s into alert %s", event.EventType, event.TenantID, existingAlertID)

			// Increment occurrences and update existing alert inside tenant context transaction
			return db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
				existingAlert, getErr := wp.alertRepo.GetByID(tenantCtx, tx, existingAlertID, event.Timestamp)
				if getErr != nil {
					// If not found in current partition (or resolved/archived), fallthrough to create a new one
					return wp.createNewAlert(tenantCtx, tx, event, debounceKey)
				}

				// Increment duplication counters inside the payload map
				if existingAlert.Payload == nil {
					existingAlert.Payload = make(map[string]interface{})
				}
				countVal, ok := existingAlert.Payload["occurrences"]
				var count float64 = 1
				if ok {
					if c, ok := countVal.(float64); ok {
						count = c + 1
					}
				} else {
					count = 2
				}
				existingAlert.Payload["occurrences"] = count
				existingAlert.Summary = event.Title // Keep the latest summary

				// If it was closed/suppressed, revive it since it occurred again
				if existingAlert.Status == model.AlertResolved {
					existingAlert.Status = model.AlertTriggered
					existingAlert.ResolvedAt = nil
				}

				// Persist updates
				if err := wp.alertRepo.Update(tenantCtx, tx, existingAlert); err != nil {
					return err
				}

				// Publish the updated alert to Redis Pub/Sub for Cockpit dynamic updates
				wp.publishToPubSub(ctx, event.TenantID, existingAlert)

				return nil
			})
		}
	}

	// DEBOUNCE MISS: This is a brand new alert. Insert and track it in Redis.
	return db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
		return wp.createNewAlert(tenantCtx, tx, event, debounceKey)
	})
}

func (wp *WorkerPool) createNewAlert(ctx context.Context, tx pgx.Tx, event model.UnifiedIncident, debounceKey string) error {
	newAlert := &model.Alert{
		ID:         event.ID,
		TenantID:   event.TenantID,
		DeviceID:   event.DeviceID,
		EventType:  event.EventType,
		Severity:   event.Severity,
		Status:     model.AlertTriggered,
		Summary:    event.Title,
		Payload:    event.RawPayload,
		CreatedAt:  event.Timestamp,
		AIAnalysis: map[string]interface{}{
			"noise_filter_applied": true,
			"source":               string(event.Source),
			"external_id":          event.ExternalID,
			"host":                 event.Host,
			"description":          event.Description,
		},
	}

	// Set starting occurrences
	if newAlert.Payload == nil {
		newAlert.Payload = make(map[string]interface{})
	}
	newAlert.Payload["occurrences"] = 1.0

	// Fetch host logs on demand from Loki if severity is warning/critical/fatal
	if event.Host != "" && (event.Severity == model.SeverityWarning || event.Severity == model.SeverityCritical || event.Severity == model.SeverityFatal) {
		logs, err := wp.lokiClient.FetchHostLogs(ctx, event.TenantID, event.Host)
		if err == nil {
			newAlert.AIAnalysis["loki_logs"] = logs
		} else {
			log.Printf("[Loki warning] Failed to query Loki logs: %v", err)
		}
	}

	// Save to DB
	if err := wp.alertRepo.Create(ctx, tx, newAlert); err != nil {
		return err
	}

	// Push to AI evaluation queue for Python worker processing
	aiPayload := map[string]interface{}{
		"alert_id":  newAlert.ID.String(),
		"tenant_id": newAlert.TenantID.String(),
	}
	aiBytes, err := json.Marshal(aiPayload)
	if err == nil {
		_ = wp.redisClient.LPush(ctx, "noc:queue:ai_evaluation", aiBytes).Err()
	}

	// Store key pointer in Redis to debounce subsequent events for the next 5 minutes
	err = wp.redisClient.Set(ctx, debounceKey, newAlert.ID.String(), DebounceTTL).Err()
	if err != nil {
		log.Printf("[Warning] Failed to write debounce TTL in Redis: %v", err)
	}

	// Publish the new alert to Redis Pub/Sub for WebSockets
	wp.publishToPubSub(ctx, event.TenantID, newAlert)

	// Trigger SRE AI Diagnosis (Gemini) asynchronously
	go func(alertID, tenantID uuid.UUID, summary, host string, payload map[string]interface{}) {
		ctxBg := context.Background()
		payloadBytes, _ := json.Marshal(payload)
		diag, err := api.DiagnoseIncident(ctxBg, summary, host, string(payloadBytes))
		if err == nil && diag != "" {
			// Save the diagnosis to the database column
			_, dbErr := wp.pgPool.Exec(ctxBg, "UPDATE alerts SET ai_diagnostic = $1 WHERE id = $2", diag, alertID)
			if dbErr != nil {
				log.Printf("[AI Co-Pilot Error] Failed to save diagnosis: %v", dbErr)
			} else {
				// Fetch the updated alert and republish it via WebSocket
				var a model.Alert
				var pBytes []byte
				var aiBytes []byte
				query := "SELECT id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at, resolved_at, acknowledged_at, ai_diagnostic FROM alerts WHERE id = $1"
				err = wp.pgPool.QueryRow(ctxBg, query, alertID).Scan(
					&a.ID,
					&a.TenantID,
					&a.DeviceID,
					&a.EventType,
					&a.Severity,
					&a.Status,
					&a.Summary,
					&pBytes,
					&aiBytes,
					&a.CreatedAt,
					&a.UpdatedAt,
					&a.ResolvedAt,
					&a.AcknowledgedAt,
					&a.AIDiagnostic,
				)
				if err == nil {
					_ = json.Unmarshal(pBytes, &a.Payload)
					if len(aiBytes) > 0 {
						_ = json.Unmarshal(aiBytes, &a.AIAnalysis)
					}
					// Publish the updated alert to Redis Pub/Sub so UI updates dynamically
					wp.publishToPubSub(ctxBg, tenantID, &a)
				}
			}
		}
	}(newAlert.ID, newAlert.TenantID, newAlert.Summary, event.Host, newAlert.Payload)

	// Trigger self healing remote script (SOAR Playbook Auto-Healing Engine) if CRITICAL/FATAL
	if newAlert.Severity == model.SeverityCritical || newAlert.Severity == model.SeverityFatal {
		wp.triggerSOARPlaybooks(ctx, newAlert)
	}

	return nil
}

func (wp *WorkerPool) publishToPubSub(ctx context.Context, tenantID uuid.UUID, alert *model.Alert) {
	alertBytes, err := json.Marshal(alert)
	if err != nil {
		return
	}

	channel := PubSubChannel + tenantID.String()
	_ = wp.redisClient.Publish(ctx, channel, alertBytes).Err()
}

func (wp *WorkerPool) triggerSOARPlaybooks(ctx context.Context, alert *model.Alert) {
	tenantCtx := db.WithTenantID(ctx, alert.TenantID)
	
	// Query auto-trigger runbooks for this tenant
	query := `
		SELECT id, name, script, vault_key_host 
		FROM tenant_runbooks 
		WHERE tenant_id = $1 AND auto_trigger = TRUE
	`
	rows, err := wp.pgPool.Query(tenantCtx, query, alert.TenantID)
	if err != nil {
		log.Printf("[SOAR Warning] Failed to query auto-trigger runbooks: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var runbookID uuid.UUID
		var name, script, vaultKeyHost string
		if err := rows.Scan(&runbookID, &name, &script, &vaultKeyHost); err != nil {
			continue
		}

		log.Printf("[SOAR Engine] Auto-triggering SOAR playbook [%s] for alert %s", name, alert.ID)

		// Execute the SOAR playbook in a background goroutine
		go func(rID uuid.UUID, rbName, rbScript, vkHost string) {
			ctxBg := db.WithTenantID(context.Background(), alert.TenantID)
			
			// 1. Fetch host SSH credentials from the Vault
			hostKey := vkHost + "_host"
			userKey := vkHost + "_user"
			passKey := vkHost + "_password"
			privKey := vkHost + "_private_key"

			var sshHost, sshUser, sshPass, sshPriv string
			
			secretQuery := `
				SELECT secret_key, decrypted_value 
				FROM decrypted_vault 
				WHERE tenant_id = $1 AND secret_key IN ($2, $3, $4, $5)
			`
			secRows, err := wp.pgPool.Query(ctxBg, secretQuery, alert.TenantID, hostKey, userKey, passKey, privKey)
			if err == nil {
				for secRows.Next() {
					var k, val string
					if err := secRows.Scan(&k, &val); err == nil {
						if k == hostKey {
							sshHost = val
						} else if k == userKey {
							sshUser = val
						} else if k == passKey {
							sshPass = val
						} else if k == privKey {
							sshPriv = val
						}
					}
				}
				secRows.Close()
			}

			if sshHost == "" || sshUser == "" {
				log.Printf("[SOAR Error] SSH host or user credentials missing in Vault for key %s", vkHost)
				return
			}

			// 2. Execute remote SSH script
			output, err := api.ExecuteSSH(sshHost, sshUser, sshPass, sshPriv, rbScript)
			execStatus := "sucesso"
			if err != nil {
				execStatus = "falha"
				output = fmt.Sprintf("SOAR Execution Error: %v\nLogs:\n%s", err, output)
			}

			// 3. Record log inside comments and execution audit logs table within a transaction
			_ = db.ExecuteInTenantTx(ctxBg, wp.pgPool, func(tx pgx.Tx) error {
				logQuery := `
					INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
					VALUES ($1, $2, '🤖 SOAR Auto-Healing Engine', $3)
				`
				commentText := fmt.Sprintf("🤖 **SOAR Playbook Auto-Trigger [%s]**: Status: %s\n\n```bash\n%s\n```", rbName, execStatus, output)
				_, err = tx.Exec(ctxBg, logQuery, alert.ID, alert.TenantID, commentText)
				if err != nil {
					return err
				}

				auditQuery := `
					INSERT INTO runbook_execution_logs (tenant_id, runbook_id, incident_id, operator_name, script, output, status)
					VALUES ($1, $2, $3, $4, $5, $6, $7)
				`
				_, err = tx.Exec(ctxBg, auditQuery, alert.TenantID, rID, alert.ID, "🤖 SOAR Auto-Healing Engine", rbScript, output, execStatus)
				if err != nil {
					return err
				}

				// If successful, resolve the alert automatically!
				if execStatus == "sucesso" {
					updateAlertQuery := `
						UPDATE alerts 
						SET status = 'resolved', resolved_at = NOW(), updated_at = NOW() 
						WHERE id = $1 AND tenant_id = $2
					`
					_, _ = tx.Exec(ctxBg, updateAlertQuery, alert.ID, alert.TenantID)
				}
				return nil
			})

			if execStatus == "falha" {
				hostStr := "unknown_host"
				if h, ok := alert.AIAnalysis["host"].(string); ok {
					hostStr = h
				}
				// Fallback ITSM escalation: spawn the Python itsm_jira_fallback.py script
				log.Printf("[SOAR Fallback] Execution failed. Escalating to Jira Service Management...")
				fallbackCmd := exec.Command("python", "./scripts/playbooks/itsm_jira_fallback.py",
					"--summary", fmt.Sprintf("SOAR Automação Falhou: %s no ativo %s", alert.Summary, hostStr),
					"--details", fmt.Sprintf("Logs:\n%s", output),
					"--severity", "critical",
					"--group", "Escalation N3 Operations",
				)
				var fallbackOut bytes.Buffer
				fallbackCmd.Stdout = &fallbackOut
				fallbackCmd.Stderr = &fallbackOut
				errFallback := fallbackCmd.Run()
				if errFallback != nil {
					log.Printf("[SOAR Fallback Error] Failed to run ITSM fallback: %v. Output: %s", errFallback, fallbackOut.String())
				} else {
					log.Printf("[SOAR Fallback Success] JSM ticket opened. Output: %s", fallbackOut.String())
				}
			}

			// Refresh alert UI via websocket
			var a model.Alert
			var pBytes []byte
			var aiBytes []byte
			queryDetails := "SELECT id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at, updated_at, resolved_at, acknowledged_at, ai_diagnostic, itsm_ticket_ref, mitre_tactics, ueba_anomalous FROM alerts WHERE id = $1"
			err = wp.pgPool.QueryRow(ctxBg, queryDetails, alert.ID).Scan(
				&a.ID,
				&a.TenantID,
				&a.DeviceID,
				&a.EventType,
				&a.Severity,
				&a.Status,
				&a.Summary,
				&pBytes,
				&aiBytes,
				&a.CreatedAt,
				&a.UpdatedAt,
				&a.ResolvedAt,
				&a.AcknowledgedAt,
				&a.AIDiagnostic,
				&a.ITSMTicketRef,
				&a.MitreTactics,
				&a.UEBAAnomalous,
			)
			if err == nil {
				_ = json.Unmarshal(pBytes, &a.Payload)
				if len(aiBytes) > 0 {
					_ = json.Unmarshal(aiBytes, &a.AIAnalysis)
				}
				wp.publishToPubSub(ctxBg, alert.TenantID, &a)
			}
		}(runbookID, name, script, vaultKeyHost)
	}
}

// StartWatchdog initializes the background heartbeat ticker checker.
func (wp *WorkerPool) StartWatchdog(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-wp.stopChan:
				ticker.Stop()
				return
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				wp.checkConnectorsHealth(ctx)
			}
		}
	}()
}

func (wp *WorkerPool) checkConnectorsHealth(ctx context.Context) {
	keys, err := wp.redisClient.Keys(ctx, "heartbeat:connector:*").Result()
	if err != nil {
		return
	}

	currentTime := time.Now().Unix()
	const limitSeconds = 600 // 10 minutes without telemetry signal

	for _, key := range keys {
		lastSeenStr, err := wp.redisClient.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		var lastSeen int64
		_, _ = fmt.Sscanf(lastSeenStr, "%d", &lastSeen)

		timeElapsed := currentTime - lastSeen
		if timeElapsed > limitSeconds {
			parts := strings.Split(key, ":")
			if len(parts) < 4 {
				continue
			}
			tenantIDStr := parts[2]
			connectorName := parts[3]

			tenantID, err := uuid.Parse(tenantIDStr)
			if err != nil {
				continue
			}

			summary := fmt.Sprintf("Watchdog Heartbeat: Perda de Telemetria / Falha de Comunicação com conector %s há %ds", connectorName, timeElapsed)

			incident := model.UnifiedIncident{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Source:    model.SourceSystem,
				EventType: "telemetry_loss_" + connectorName,
				Severity:  model.SeverityCritical,
				Title:     summary,
				Timestamp: time.Now(),
				Host:      "watchdog",
			}

			_ = wp.processEvent(ctx, incident)
		}
	}
}

// StartMappingEngine runs a background thread that translates raw messages into normalized UnifiedIncident structures.
func (wp *WorkerPool) StartMappingEngine(ctx context.Context) {
	wp.wg.Add(1)
	go func() {
		defer wp.wg.Done()
		log.Println("[Mapping Engine] Ingestion Data Mapping Consumer started...")

		for {
			select {
			case <-wp.stopChan:
				return
			case <-ctx.Done():
				return
			default:
				// BRPOP raw events
				result, err := wp.redisClient.BRPop(ctx, 2*time.Second, api.AlertsRawQueueKey).Result()
				if err != nil {
					if err == redis.Nil {
						continue
					}
					log.Printf("[Mapping Engine Error] BRPOP failed: %v", err)
					time.Sleep(1 * time.Second)
					continue
				}

				if len(result) < 2 {
					continue
				}

				rawBytes := []byte(result[1])
				var rawMsg api.RawWebhookMessage
				if err := json.Unmarshal(rawBytes, &rawMsg); err != nil {
					// Parse failure: write to DLQ
					log.Printf("[Mapping Engine] Malformed JSON raw message, writing to DLQ")
					_ = wp.redisClient.LPush(ctx, api.AlertsDLQQueueKey, rawBytes).Err()
					continue
				}

				// Map to universal incident schema based on integration type
				incident, err := wp.mapToUniversalSchema(ctx, rawMsg)
				if err != nil {
					// Mapping failure: write to DLQ
					log.Printf("[Mapping Engine] Mapping failed: %v. Writing to DLQ.", err)
					_ = wp.redisClient.LPush(ctx, api.AlertsDLQQueueKey, rawBytes).Err()
					continue
				}

				// Push mapped incident to alerts.normalized queue
				incidentBytes, _ := json.Marshal(incident)
				err = wp.redisClient.LPush(ctx, api.AlertsNormalizedQueueKey, incidentBytes).Err()
				if err != nil {
					log.Printf("[Mapping Engine] Failed to push normalized event: %v", err)
				}
			}
		}
	}()
}

func (wp *WorkerPool) mapToUniversalSchema(ctx context.Context, rawMsg api.RawWebhookMessage) (model.UnifiedIncident, error) {
	payloadBytes, err := json.Marshal(rawMsg.Payload)
	if err != nil {
		return model.UnifiedIncident{}, err
	}

	var incident model.UnifiedIncident

	switch strings.ToLower(rawMsg.IntegrationType) {
	case "prometheus":
		var pPayload api.AlertmanagerPayload
		if err := json.Unmarshal(payloadBytes, &pPayload); err == nil && len(pPayload.Alerts) > 0 {
			incident = api.MapPrometheusToUnified(pPayload.Alerts[0], rawMsg.TenantID)
		} else {
			var alert api.AlertmanagerAlert
			if err := json.Unmarshal(payloadBytes, &alert); err != nil {
				return model.UnifiedIncident{}, err
			}
			incident = api.MapPrometheusToUnified(alert, rawMsg.TenantID)
		}
	case "wazuh":
		var wPayload api.WazuhAlertPayload
		if err := json.Unmarshal(payloadBytes, &wPayload); err != nil {
			return model.UnifiedIncident{}, err
		}
		incident = api.MapWazuhToUnified(wPayload, rawMsg.TenantID)
	case "uptimekuma":
		var uPayload api.UptimeKumaPayload
		if err := json.Unmarshal(payloadBytes, &uPayload); err != nil {
			return model.UnifiedIncident{}, err
		}
		incident = api.MapUptimeKumaToUnified(uPayload, rawMsg.TenantID)
	case "grafana":
		var gPayload api.AlertmanagerPayload
		if err := json.Unmarshal(payloadBytes, &gPayload); err == nil && len(gPayload.Alerts) > 0 {
			incident = api.MapGrafanaToUnified(gPayload.Alerts[0], rawMsg.TenantID)
		} else {
			var alert api.AlertmanagerAlert
			if err := json.Unmarshal(payloadBytes, &alert); err != nil {
				return model.UnifiedIncident{}, err
			}
			incident = api.MapGrafanaToUnified(alert, rawMsg.TenantID)
		}
	case "zabbix":
		var zPayload api.ZabbixPayload
		if err := json.Unmarshal(payloadBytes, &zPayload); err != nil {
			return model.UnifiedIncident{}, err
		}
		incident = api.MapZabbixToUnified(zPayload, rawMsg.TenantID)
	default:
		incident = model.UnifiedIncident{
			ID:        uuid.New(),
			TenantID:  rawMsg.TenantID,
			Source:    model.IncidentSource(rawMsg.IntegrationType),
			EventType: "generic_webhook_event",
			Severity:  model.SeverityInfo,
			Title:     "Generic Ingested Alert",
			Timestamp: time.Now(),
			RawPayload: rawMsg.Payload,
		}
	}

	return incident, nil
}
