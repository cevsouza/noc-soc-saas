package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/cache"
	"noc-api/internal/connector"
	"noc-api/internal/db"
	"noc-api/internal/executor"
	"noc-api/internal/loki"
	"noc-api/internal/model"
	"noc-api/internal/notifier"
	"noc-api/internal/queue"
	"noc-api/internal/repository"
	"noc-api/internal/threatintel"
	"noc-api/internal/security"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	// v2: bumped from "noc:debounce:" so any leftover keys in the old
	// tenant:device:event_type format age out harmlessly during rollout instead of ever being
	// compared against the new fingerprint-keyed, JSON-envelope-valued format.
	DebounceKeyPrefix = "noc:debounce:v2:"
	DebounceTTL       = 5 * time.Minute
	PubSubChannel     = "noc:pubsub:tenant:"
)

type WorkerPool struct {
	pgPool            *pgxpool.Pool
	redisClient       *redis.Client
	alertRepo         repository.AlertRepository
	vaultRepo         repository.VaultRepository
	numWorkers        int
	wg                sync.WaitGroup
	stopChan          chan struct{}
	executor          *executor.SelfHealingExecutor
	lokiClient        *loki.LokiClient
	pagerDutyNotifier *notifier.PagerDutyNotifier
	opsgenieNotifier  *notifier.OpsgenieNotifier
	slackNotifier     *notifier.SlackNotifier
	teamsNotifier     *notifier.TeamsNotifier
	emailNotifier     *notifier.EmailNotifier
}

func NewWorkerPool(pgPool *pgxpool.Pool, redisClient *redis.Client, numWorkers int) *WorkerPool {
	return &WorkerPool{
		pgPool:            pgPool,
		redisClient:       redisClient,
		alertRepo:         repository.NewPostgresAlertRepository(),
		vaultRepo:         repository.NewPostgresVaultRepository(),
		numWorkers:        numWorkers,
		stopChan:          make(chan struct{}),
		executor:          executor.NewSelfHealingExecutor(pgPool),
		lokiClient:        loki.NewLokiClient(pgPool),
		pagerDutyNotifier: notifier.NewPagerDutyNotifier(),
		opsgenieNotifier:  notifier.NewOpsgenieNotifier(),
		slackNotifier:     notifier.NewSlackNotifier(),
		teamsNotifier:     notifier.NewTeamsNotifier(),
		emailNotifier:     notifier.NewEmailNotifier(),
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
			result, err := wp.redisClient.BRPop(ctx, 2*time.Second, queue.AlertsNormalizedQueueKey).Result()
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

	// Temporal suppression (Fase 3/3d): drop events matching an active tenant suppression rule
	// (e.g. a maintenance window) before they ever become an alert/incident. System-generated
	// alerts (the connection watchdog) are never suppressed — losing a "source is down" signal to a
	// suppression rule would defeat the whole point.
	if event.Source != model.SourceSystem {
		if rules := wp.loadSuppressionRules(tenantCtx, event.TenantID); len(rules) > 0 {
			fields := map[string]string{
				"event_type": event.EventType,
				"host":       event.Host,
				"summary":    event.Title,
				"source":     string(event.Source),
				"severity":   string(event.Severity),
			}
			if eventSuppressed(rules, fields, time.Now()) {
				wp.redisClient.Incr(ctx, cache.TenantKey(event.TenantID, "suppression", "count"))
				log.Printf("[Suppression] Suppressed event for tenant %s (event_type=%q, host=%q)", event.TenantID, event.EventType, event.Host)
				return nil
			}
		}
	}

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

	// 1. Dedupe by content fingerprint (tenant+source+external_id, or a fallback seed when
	// external_id is absent — see computeFingerprint in fingerprint.go).
	fingerprint := computeFingerprint(event)
	debounceKey := DebounceKeyPrefix + fingerprint

	// Try reading the debounce pointer from Redis. It's a JSON envelope carrying both the
	// alert's ID and its real created_at: GetByID needs an exact created_at match for
	// partition pruning, so the pointer must record the value actually written at creation,
	// not the new (duplicate) event's own timestamp.
	pointerStr, err := wp.redisClient.Get(ctx, debounceKey).Result()
	if err == nil && pointerStr != "" {
		var ptr debouncePointer
		if unmarshalErr := json.Unmarshal([]byte(pointerStr), &ptr); unmarshalErr == nil {
			// DEBOUNCE HIT: An active alert with this fingerprint exists in the last 5 minutes.
			log.Printf("[Debounce Engine] Hit! Grouping fingerprint '%s' for tenant %s into alert %s", fingerprint, event.TenantID, ptr.AlertID)

			// Increment occurrences and update existing alert inside tenant context transaction
			return db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
				existingAlert, getErr := wp.alertRepo.GetByID(tenantCtx, tx, ptr.AlertID, ptr.CreatedAt)
				if getErr != nil {
					// If not found (resolved/archived/pruned), fall through to create a new one
					return wp.createNewAlert(tenantCtx, tx, event, debounceKey, fingerprint)
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
	if err := db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
		return wp.createNewAlert(tenantCtx, tx, event, debounceKey, fingerprint)
	}); err != nil {
		return err
	}

	// Cross-tenant threat intel (Backlog B6). Best-effort and post-commit: gated on the tenant's
	// opt-in, only public IPs/domains/hashes are shared, and a failure here never affects alert
	// creation. Consumes the aggregate (risk enrichment) before contributing this event's own sighting.
	wp.contributeThreatIntel(ctx, tenantCtx, event, fingerprint)
	return nil
}

// contributeThreatIntel records the event's shareable indicators into the global threat-intel
// aggregate (opt-in only) and enriches the incident's risk when those indicators have cross-tenant
// corroboration. Never fatal.
func (wp *WorkerPool) contributeThreatIntel(ctx, tenantCtx context.Context, event model.UnifiedIncident, fingerprint string) {
	var optIn bool
	if err := wp.pgPool.QueryRow(ctx, `SELECT threat_intel_opt_in FROM tenants WHERE id = $1`, event.TenantID).Scan(&optIn); err != nil || !optIn {
		return
	}
	indicators := threatintel.ExtractIndicators(event.Host, event.RawPayload)
	if len(indicators) == 0 {
		return
	}
	tenantHash := threatintel.TenantHash(event.TenantID)

	// Consume BEFORE contributing so this tenant's own new sighting isn't counted as corroboration.
	others, err := threatintel.OtherTenantSightings(ctx, wp.pgPool, tenantHash, indicators)
	if err != nil {
		log.Printf("[ThreatIntel warning] sightings lookup failed for tenant %s: %v", event.TenantID, err)
	}

	if err := threatintel.Record(ctx, wp.pgPool, tenantHash, indicators); err != nil {
		log.Printf("[ThreatIntel warning] contribution failed for tenant %s: %v", event.TenantID, err)
	}

	if bonus := threatintel.FleetRiskBonus(others); bonus > 0 {
		wp.enrichIncidentWithFleetIntel(tenantCtx, event, fingerprint, indicators, others, bonus)
	}
}

// enrichIncidentWithFleetIntel raises the grouped incident's risk score and drops an investigation
// note when the event's IOCs have cross-tenant corroboration (seen by other opted-in tenants).
// Best-effort inside the tenant RLS tx; a failure only skips the enrichment.
func (wp *WorkerPool) enrichIncidentWithFleetIntel(tenantCtx context.Context, event model.UnifiedIncident, fingerprint string, indicators []threatintel.Indicator, others, bonus int) {
	err := db.ExecuteInTenantTx(tenantCtx, wp.pgPool, func(tx pgx.Tx) error {
		var incID uuid.UUID
		e := tx.QueryRow(tenantCtx, `
			SELECT id FROM incidents
			WHERE tenant_id = $1 AND fingerprint = $2 AND status <> 'resolved'
			ORDER BY last_seen DESC LIMIT 1
		`, event.TenantID, fingerprint).Scan(&incID)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		if e != nil {
			return e
		}
		if _, e := tx.Exec(tenantCtx, `UPDATE incidents SET risk_score = LEAST(100, risk_score + $2), updated_at = NOW() WHERE id = $1 AND tenant_id = $3`, incID, bonus, event.TenantID); e != nil {
			return e
		}
		note := fmt.Sprintf("🛡️ Threat intel: %s visto por %d outro(s) tenant(s) na frota — risco +%d (confiança cross-tenant).",
			threatintel.Summarize(indicators), others, bonus)
		_, e = tx.Exec(tenantCtx, `INSERT INTO incident_comments (incident_id, tenant_id, author, comment) VALUES ($1, $2, '🛡️ Threat Intel (frota)', $3)`, incID, event.TenantID, note)
		return e
	})
	if err != nil {
		log.Printf("[ThreatIntel warning] enrichment failed for tenant %s: %v", event.TenantID, err)
	}
}

func (wp *WorkerPool) createNewAlert(ctx context.Context, tx pgx.Tx, event model.UnifiedIncident, debounceKey string, fingerprint string) error {
	newAlert := &model.Alert{
		ID:          event.ID,
		TenantID:    event.TenantID,
		DeviceID:    event.DeviceID,
		EventType:   event.EventType,
		Severity:    event.Severity,
		Status:      model.AlertTriggered,
		Summary:     event.Title,
		Payload:     event.RawPayload,
		CreatedAt:   event.Timestamp,
		Fingerprint: fingerprint,
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

	// Group this alert into an incident (find-or-create an open incident for its fingerprint) and
	// link it. Runs inside a SAVEPOINT so a grouping failure rolls back only the incident work and
	// never drops the alert itself.
	wp.attachIncident(ctx, tx, newAlert, fingerprint)

	// Push to AI evaluation queue for Python worker processing
	aiPayload := map[string]interface{}{
		"alert_id":  newAlert.ID.String(),
		"tenant_id": newAlert.TenantID.String(),
	}
	aiBytes, err := json.Marshal(aiPayload)
	if err == nil {
		_ = wp.redisClient.LPush(ctx, "noc:queue:ai_evaluation", aiBytes).Err()
	}

	// Store the debounce pointer as a JSON envelope carrying the alert's real created_at (as
	// actually persisted, which may differ slightly from event.Timestamp) — GetByID requires
	// an exact match on this value for partition pruning, and using the wrong value here was a
	// real bug: dedupe hits silently fell through to creating a duplicate alert whenever the
	// new event's own timestamp didn't exactly match the original row's created_at.
	ptr := debouncePointer{AlertID: newAlert.ID, CreatedAt: newAlert.CreatedAt}
	if ptrBytes, err := json.Marshal(ptr); err != nil {
		log.Printf("[Warning] Failed to marshal debounce pointer: %v", err)
	} else if err := wp.redisClient.Set(ctx, debounceKey, ptrBytes, DebounceTTL).Err(); err != nil {
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

	// Trigger self healing remote script (SOAR Playbook Auto-Healing Engine) if CRITICAL/FATAL.
	// System-generated alerts (the connection watchdog) must escalate so a human is paged, but
	// must NOT auto-run SOAR remediation runbooks — those target infra faults, not a connectivity
	// gap, and triggerSOARPlaybooks fires every auto_trigger runbook regardless of the alert's
	// subject. So we page but skip remediation for source=system.
	if newAlert.Severity == model.SeverityCritical || newAlert.Severity == model.SeverityFatal {
		if event.Source != model.SourceSystem {
			wp.triggerSOARPlaybooks(ctx, newAlert)
		}
		wp.triggerEscalations(ctx, newAlert)
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
		SELECT id, name, script, vault_key_host, is_safe
		FROM tenant_runbooks
		WHERE tenant_id = $1 AND auto_trigger = TRUE
	`
	rows, err := wp.pgPool.Query(tenantCtx, query, alert.TenantID)
	if err != nil {
		log.Printf("[SOAR Warning] Failed to query auto-trigger runbooks: %v", err)
		return
	}
	defer rows.Close()

	type candidate struct {
		id           uuid.UUID
		name         string
		script       string
		vaultKeyHost string
		isSafe       bool
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.name, &c.script, &c.vaultKeyHost, &c.isSafe); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	for _, c := range candidates {
		// SECURITY: severity fatal always requires human approval, regardless of is_safe.
		// Severity critical only auto-executes if the runbook has been explicitly reviewed
		// and marked is_safe=true; otherwise it also becomes an approval request. This is
		// secure-by-default — existing runbooks default to is_safe=false until an admin
		// reviews and opts them in.
		requiresApproval := alert.Severity == model.SeverityFatal || !c.isSafe
		if requiresApproval {
			wp.createRunbookApprovalRequest(tenantCtx, c.id, c.name, alert)
			continue
		}

		log.Printf("[SOAR Engine] Auto-triggering SOAR playbook [%s] for alert %s", c.name, alert.ID)

		// Execute the SOAR playbook in a background goroutine
		go func(rID uuid.UUID, rbName, rbScript, vkHost string) {
			ctxBg := db.WithTenantID(context.Background(), alert.TenantID)

			// 1. Fetch host SSH credentials from the Vault (decrypted in application code —
			// there is no "decrypted_vault" view/table; secrets are AES-GCM encrypted at rest).
			masterKey, err := security.GetMasterKey()
			if err != nil {
				log.Printf("[SOAR Error] Failed to retrieve vault master key: %v", err)
				return
			}

			var sshHost, sshUser, sshPass, sshPriv string
			err = db.ExecuteInTenantTx(ctxBg, wp.pgPool, func(tx pgx.Tx) error {
				getSecret := func(key string) string {
					sec, err := wp.vaultRepo.GetSecretByKey(ctxBg, tx, key)
					if err != nil {
						return ""
					}
					decrypted, err := security.DecryptForTenant(sec.EncryptedValue, sec.Nonce, masterKey, alert.TenantID)
					if err != nil {
						return ""
					}
					return string(decrypted)
				}
				sshHost = getSecret(vkHost + "_host")
				sshUser = getSecret(vkHost + "_user")
				sshPass = getSecret(vkHost + "_password")
				sshPriv = getSecret(vkHost + "_private_key")
				return nil
			})

			if err != nil || sshHost == "" || sshUser == "" {
				log.Printf("[SOAR Error] SSH host or user credentials missing in Vault for key %s", vkHost)
				return
			}

			// 2. Execute remote SSH script
			output, err := api.ExecuteSSH(ctxBg, wp.pgPool, wp.vaultRepo, alert.TenantID, vkHost, sshHost, sshUser, sshPass, sshPriv, rbScript)
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
				_, err = tx.Exec(ctxBg, logQuery, incidentRef(alert), alert.TenantID, commentText)
				if err != nil {
					return err
				}

				auditQuery := `
					INSERT INTO runbook_execution_logs (tenant_id, runbook_id, incident_id, operator_name, script, output, status)
					VALUES ($1, $2, $3, $4, $5, $6, $7)
				`
				_, err = tx.Exec(ctxBg, auditQuery, alert.TenantID, rID, incidentRef(alert), "🤖 SOAR Auto-Healing Engine", rbScript, output, execStatus)
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
		}(c.id, c.name, c.script, c.vaultKeyHost)
	}
}

// escalationVaultKeys maps a tenant_integrations.type value to the vault secret_key holding
// its outbound escalation credential (routing key / API key). A single "pagerduty"/"opsgenie"
// integration type covers both inbound webhook ingestion (see internal/connector) and this
// outbound escalation — confirmed with the user as the simpler default for v1.
var escalationVaultKeys = map[string]string{
	"pagerduty": "pagerduty_routing_key",
	"opsgenie":  "opsgenie_api_key",
	"slack":     "slack_webhook_url",
	"teams":     "teams_webhook_url",
	// "email" isn't really a "secret" — it's the tenant's configured recipient address — but it
	// reuses the same vault/tenant_integrations mechanism as the others for UI consistency
	// (SMTP credentials themselves stay a platform-wide env var, not per-tenant).
	"email": "email_recipient",
}

// triggerEscalations pages out via PagerDuty/Opsgenie for any tenant that has one of those
// integrations active, mirroring triggerSOARPlaybooks' shape (a synchronous candidate query,
// then one goroutine per candidate doing vault decrypt + the actual outbound call). A failed
// escalation only logs — v1 has no retry queue, since the alert stays fully visible in the
// cockpit regardless of whether the page went out.
func (wp *WorkerPool) triggerEscalations(ctx context.Context, alert *model.Alert) {
	tenantCtx := db.WithTenantID(ctx, alert.TenantID)

	rows, err := wp.pgPool.Query(tenantCtx,
		`SELECT type FROM tenant_integrations WHERE tenant_id = $1 AND type IN ('pagerduty', 'opsgenie', 'slack', 'teams', 'email') AND status = 'active'`,
		alert.TenantID)
	if err != nil {
		log.Printf("[Escalation Warning] Failed to query active escalation integrations: %v", err)
		return
	}
	var activeTypes []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			continue
		}
		activeTypes = append(activeTypes, t)
	}
	rows.Close()

	for _, integrationType := range activeTypes {
		go func(itype string) {
			ctxBg := db.WithTenantID(context.Background(), alert.TenantID)

			vaultKey, ok := escalationVaultKeys[itype]
			if !ok {
				return
			}

			masterKey, err := security.GetMasterKey()
			if err != nil {
				log.Printf("[Escalation Error] Failed to retrieve vault master key: %v", err)
				return
			}

			var secretVal string
			err = db.ExecuteInTenantTx(ctxBg, wp.pgPool, func(tx pgx.Tx) error {
				sec, err := wp.vaultRepo.GetSecretByKey(ctxBg, tx, vaultKey)
				if err != nil {
					return err
				}
				decrypted, err := security.DecryptForTenant(sec.EncryptedValue, sec.Nonce, masterKey, alert.TenantID)
				if err != nil {
					return err
				}
				secretVal = string(decrypted)
				return nil
			})
			if err != nil || secretVal == "" {
				log.Printf("[Escalation Error] Missing/undecryptable %s credential for tenant %s: %v", vaultKey, alert.TenantID, err)
				return
			}

			var notifyErr error
			switch itype {
			case "pagerduty":
				notifyErr = wp.pagerDutyNotifier.Notify(ctxBg, secretVal, alert)
			case "opsgenie":
				notifyErr = wp.opsgenieNotifier.Notify(ctxBg, secretVal, alert)
			case "slack":
				notifyErr = wp.slackNotifier.Notify(ctxBg, secretVal, alert)
			case "teams":
				notifyErr = wp.teamsNotifier.Notify(ctxBg, secretVal, alert)
			case "email":
				notifyErr = wp.emailNotifier.Notify(ctxBg, secretVal, alert)
			}
			if notifyErr != nil {
				log.Printf("[Escalation Error] %s escalation failed for alert %s (tenant %s): %v", itype, alert.ID, alert.TenantID, notifyErr)
			} else {
				log.Printf("[Escalation] %s escalation sent for alert %s (tenant %s)", itype, alert.ID, alert.TenantID)
			}
		}(integrationType)
	}
}

// createRunbookApprovalRequest records that an auto-trigger runbook was withheld pending
// human review (because the alert is fatal, or the runbook isn't marked is_safe), and posts
// a timeline comment so operators see it needs attention in Runbooks > Aprovações.
func (wp *WorkerPool) createRunbookApprovalRequest(ctx context.Context, runbookID uuid.UUID, runbookName string, alert *model.Alert) {
	reason := fmt.Sprintf("Auto-trigger de severidade %s exige aprovação humana antes da execução.", alert.Severity)

	err := db.ExecuteInTenantTx(ctx, wp.pgPool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO runbook_approval_requests (tenant_id, runbook_id, incident_id, reason, requested_by)
			VALUES ($1, $2, $3, $4, 'SOAR Auto-Healing Engine')
		`, alert.TenantID, runbookID, incidentRef(alert), reason)
		if err != nil {
			return err
		}

		commentText := fmt.Sprintf("⏸️ **Aprovação necessária**: o runbook [%s] seria auto-executado, mas requer aprovação humana (severidade %s ou runbook ainda não marcado como seguro). Revise em Runbooks > Aprovações.", runbookName, alert.Severity)
		_, err = tx.Exec(ctx, `
			INSERT INTO incident_comments (incident_id, tenant_id, author, comment)
			VALUES ($1, $2, '🤖 SOAR Auto-Healing Engine', $3)
		`, incidentRef(alert), alert.TenantID, commentText)
		return err
	})

	if err != nil {
		log.Printf("[SOAR Approval] Failed to create approval request for runbook %s: %v", runbookID, err)
		return
	}
	log.Printf("[SOAR Approval] Runbook '%s' for alert %s requires human approval before execution", runbookName, alert.ID)
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
				result, err := wp.redisClient.BRPop(ctx, 2*time.Second, queue.AlertsRawQueueKey).Result()
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
				var rawMsg queue.RawWebhookMessage
				if err := json.Unmarshal(rawBytes, &rawMsg); err != nil {
					// Parse failure: write to DLQ
					log.Printf("[Mapping Engine] Malformed JSON raw message, writing to DLQ")
					wp.pushToDLQ(ctx, rawBytes, "malformed_json", err)
					continue
				}

				// Map to universal incident schema based on integration type
				incidents, err := wp.mapToUniversalSchema(rawMsg)
				if err != nil {
					// Mapping failure: write to DLQ
					log.Printf("[Mapping Engine] Mapping failed: %v. Writing to DLQ.", err)
					wp.pushToDLQ(ctx, rawBytes, "mapping_failed", err)
					continue
				}

				// Push each mapped incident to alerts.normalized queue (usually one, but a
				// tenant posting a full Alertmanager batch through the generic webhook yields
				// several from a single raw message).
				for _, incident := range incidents {
					incidentBytes, _ := json.Marshal(incident)
					if err := wp.redisClient.LPush(ctx, queue.AlertsNormalizedQueueKey, incidentBytes).Err(); err != nil {
						log.Printf("[Mapping Engine] Failed to push normalized event: %v", err)
					}
				}
			}
		}
	}()
}

// pushToDLQ wraps the raw bytes in a queue.DLQEntry and pushes it to the DLQ, logging (but not
// failing the caller) if the Redis write itself errors.
func (wp *WorkerPool) pushToDLQ(ctx context.Context, rawBytes []byte, reason string, cause error) {
	entry := queue.DLQEntry{
		Payload:  json.RawMessage(rawBytes),
		Reason:   reason,
		Error:    cause.Error(),
		FailedAt: time.Now(),
	}
	if err := queue.PushToDLQ(ctx, wp.redisClient, entry); err != nil {
		log.Printf("[Mapping Engine] Failed to write DLQ entry: %v", err)
	}
}

// mapToUniversalSchema resolves a WebhookConnector for the raw message's integration type and
// delegates the actual mapping to it (shared with internal/api's DLQ replay endpoint via
// connector.MapRawPayload, so both go through identical dispatch logic).
func (wp *WorkerPool) mapToUniversalSchema(rawMsg queue.RawWebhookMessage) ([]model.UnifiedIncident, error) {
	return connector.MapRawPayload(rawMsg.IntegrationType, rawMsg.Payload, rawMsg.TenantID)
}
