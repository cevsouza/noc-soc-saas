package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/db"
	"noc-api/internal/executor"
	"noc-api/internal/loki"
	"noc-api/internal/model"
	"noc-api/internal/repository"

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
			result, err := wp.redisClient.BRPop(ctx, 2*time.Second, api.AlertsQueueKey).Result()
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
				defer secRows.Close()
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
