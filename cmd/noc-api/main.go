package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/connector"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	"noc-api/internal/model"
	redisclient "noc-api/internal/redis"
	"noc-api/internal/repository"
	"noc-api/internal/worker"
	"noc-api/internal/ws"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	log.Println("Initializing NOC SaaS Core Engine...")

	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		jwtSecret = []byte("noc-secret-key-1234567890-super-safe")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Load PostgreSQL Connection (Support direct DATABASE_URL or fallback to parameters)
	var pgPool *pgxpool.Pool
	var err error
	databaseURL := getEnv("DATABASE_URL", "")

	if databaseURL != "" {
		log.Println("DATABASE_URL detected. Connecting to PostgreSQL using direct connection string...")
		poolCfg, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			log.Fatalf("Fatal: Failed to parse DATABASE_URL: %v", err)
		}
		// Optimize pool settings for SRE performance
		poolCfg.MaxConns = 50
		poolCfg.MinConns = 10
		poolCfg.MaxConnIdleTime = 15 * time.Minute
		poolCfg.MaxConnLifetime = 1 * time.Hour
		poolCfg.HealthCheckPeriod = 1 * time.Minute

		pgPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			log.Fatalf("Fatal: Failed to create connection pool from DATABASE_URL: %v", err)
		}
	} else {
		log.Println("No DATABASE_URL detected. Falling back to individual DB_HOST variables...")
		dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
		dbCfg := db.Config{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     dbPort,
			User:     getEnv("DB_USER", "postgres"),
			Password: getEnv("DB_PASSWORD", "postgres"),
			DBName:   getEnv("DB_NAME", "noc"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		}
		pgPool, err = db.NewConnectionPool(ctx, dbCfg)
		if err != nil {
			log.Fatalf("Fatal: Database initialization failed: %v", err)
		}
	}
	defer pgPool.Close()

	// Verify DB Connection with robust retry loop (SRE resilience pattern)
	var dbPingErr error
	for attempt := 1; attempt <= 10; attempt++ {
		log.Printf("Verifying PostgreSQL connection (attempt %d/10)...", attempt)
		if dbPingErr = pgPool.Ping(ctx); dbPingErr == nil {
			break
		}
		log.Printf("PostgreSQL ping failed (retrying in 3s): %v", dbPingErr)
		time.Sleep(3 * time.Second)
	}
	if dbPingErr != nil {
		log.Fatalf("Fatal: Failed to ping PostgreSQL database after 10 attempts: %v", dbPingErr)
	}
	log.Println("PostgreSQL Connection Pool verified successfully.")

	// 1.5 Run Schema Migrations (Embedded SQL up scripts)
	if err := db.RunMigrations(ctx, pgPool); err != nil {
		log.Fatalf("Fatal: Database migration failed: %v", err)
	}


	// 2. Load Redis Connection (Support direct REDIS_URL or fallback to parameters)
	var redisClient *redis.Client
	redisURL := getEnv("REDIS_URL", "")

	if redisURL != "" {
		log.Println("REDIS_URL detected. Connecting to Redis using direct connection URL...")
		opt, err := redis.ParseURL(redisURL)
		if err != nil {
			log.Fatalf("Fatal: Failed to parse REDIS_URL: %v", err)
		}
		opt.DialTimeout = 5 * time.Second
		opt.ReadTimeout = 3 * time.Second
		opt.WriteTimeout = 3 * time.Second
		opt.PoolSize = 50
		opt.MinIdleConns = 10

		redisClient = redis.NewClient(opt)
	} else {
		log.Println("No REDIS_URL detected. Falling back to individual REDIS_HOST variables...")
		redisPort, _ := strconv.Atoi(getEnv("REDIS_PORT", "6379"))
		redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
		redisCfg := redisclient.Config{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     redisPort,
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       redisDB,
		}
		redisClient, err = redisclient.NewRedisClient(ctx, redisCfg)
		if err != nil {
			log.Fatalf("Fatal: Redis initialization failed: %v", err)
		}
	}
	defer redisClient.Close()

	// Verify Redis Connection with robust retry loop (SRE resilience pattern)
	var redisPingErr error
	for attempt := 1; attempt <= 10; attempt++ {
		log.Printf("Verifying Redis connection (attempt %d/10)...", attempt)
		if redisPingErr = redisClient.Ping(ctx).Err(); redisPingErr == nil {
			break
		}
		log.Printf("Redis ping failed (retrying in 3s): %v", redisPingErr)
		time.Sleep(3 * time.Second)
	}
	if redisPingErr != nil {
		log.Fatalf("Fatal: Failed to ping Redis server after 10 attempts: %v", redisPingErr)
	}
	log.Println("Redis Client verified successfully.")

	serverPort := getEnv("PORT", getEnv("SERVER_PORT", "8080"))
	numWorkers, _ := strconv.Atoi(getEnv("WORKER_POOL_SIZE", "10"))

	// 4. Initialize & Start Concurrent Worker Pool
	wp := worker.NewWorkerPool(pgPool, redisClient, numWorkers)
	wp.Start(ctx)
	wp.StartWatchdog(ctx)
	wp.StartMappingEngine(ctx)
	defer wp.Stop()

	// 5. Initialize & Start WebSocket Infrastructure (SRE Multiplexed Pattern)
	hub := ws.NewHub()
	go hub.Run(ctx)
	go ws.StartGlobalPubSubMultiplexer(ctx, redisClient, hub)

	// 5.5 Start Microsoft Sentinel Background Connector
	sentinelConn := connector.NewSentinelConnector(pgPool, redisClient)
	sentinelConn.Start(ctx, 30*time.Second)
	defer sentinelConn.Stop()

	// 6. Setup HTTP Router & Middleware
	mux := http.NewServeMux()

	// Welcome and root endpoint (to avoid scary 404 page not found)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>NOC SaaS Core API</title>
    <style>
        body {
            background-color: #0b0f19;
            color: #f3f4f6;
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            display: flex;
            align-items: center;
            justify-content: center;
            min-height: 100vh;
            margin: 0;
            padding: 20px;
            box-sizing: border-box;
        }
        .card {
            background: rgba(17, 24, 39, 0.7);
            border: 1px solid rgba(59, 130, 246, 0.3);
            border-radius: 16px;
            padding: 40px;
            max-width: 500px;
            width: 100%;
            text-align: center;
            box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.3), 0 0 15px 1px rgba(59, 130, 246, 0.15);
            backdrop-filter: blur(12px);
        }
        .glow {
            width: 80px;
            height: 80px;
            background: radial-gradient(circle, #3b82f6 0%, transparent 70%);
            margin: 0 auto 20px;
            position: relative;
        }
        .glow::after {
            content: "⚡";
            font-size: 40px;
            position: absolute;
            top: 50%;
            left: 50%;
            transform: translate(-50%, -50%);
        }
        h1 {
            font-size: 24px;
            margin: 0 0 10px;
            background: linear-gradient(to right, #60a5fa, #3b82f6);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            font-weight: 700;
        }
        .status {
            display: inline-flex;
            align-items: center;
            background: rgba(16, 185, 129, 0.1);
            color: #10b981;
            padding: 4px 12px;
            border-radius: 9999px;
            font-size: 14px;
            font-weight: 500;
            margin-bottom: 20px;
            border: 1px solid rgba(16, 185, 129, 0.2);
        }
        .status::before {
            content: "";
            width: 8px;
            height: 8px;
            background-color: #10b981;
            border-radius: 50%;
            margin-right: 8px;
            box-shadow: 0 0 8px #10b981;
        }
        p {
            color: #9ca3af;
            font-size: 16px;
            line-height: 1.5;
            margin: 0 0 30px;
        }
        .btn {
            display: inline-block;
            background: linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%);
            color: white;
            padding: 12px 24px;
            border-radius: 8px;
            text-decoration: none;
            font-weight: 600;
            transition: all 0.2s ease;
            box-shadow: 0 4px 6px -1px rgba(59, 130, 246, 0.2);
        }
        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 10px 15px -3px rgba(59, 130, 246, 0.3);
        }
        .footer {
            margin-top: 30px;
            font-size: 12px;
            color: #4b5563;
        }
    </style>
</head>
<body>
    <div class="card">
        <div class="glow"></div>
        <h1>NOC SaaS Core API</h1>
        <div class="status">Online & Operational</div>
        <p>This is the core REST & WebSocket API gateway. Use the Cockpit frontend to visualize alerts, connect tenants, and manage SSH remediation runbooks.</p>
        <a href="/health" class="btn">View API Health</a>
        <div class="footer">Powered by Antigravity Core Engine</div>
    </div>
</body>
</html>`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"name": "NOC SaaS Core API",
			"status": "online",
			"version": "1.0.0",
			"health_check": "/health",
			"documentation": "https://github.com/cevsouza/noc-soc-saas",
			"message": "NOC SaaS API Gateway is fully operational. Access the Cockpit UI to interact."
		}`))
	})

	// Health Check endpoint (unauthenticated)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","uptime":"online"}`))
	})

	// High-Performance Ingestion endpoint (protected by API Key auth middleware & rate limiter)
	ingestHandler := api.HandleIngest(pgPool, redisClient)
	protectedIngest := middleware.APIKeyAuth(pgPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(ingestHandler))
	mux.Handle("/api/v1/ingest", protectedIngest)

	// High-Performance Prometheus Alertmanager & Wazuh Webhook Ingestion
	promHandler := api.HandlePrometheusIngest(pgPool, redisClient)
	protectedProm := middleware.APIKeyAuth(pgPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(promHandler))
	mux.Handle("/api/v1/ingest/prometheus", protectedProm)

	wazuhHandler := api.HandleWazuhIngest(pgPool, redisClient)
	protectedWazuh := middleware.APIKeyAuth(pgPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(wazuhHandler))
	mux.Handle("/api/v1/ingest/wazuh", protectedWazuh)

	// High-Performance Uptime Kuma, Grafana & Zabbix Webhook Ingestions
	uptimekumaHandler := api.HandleUptimeKumaIngest(pgPool, redisClient)
	protectedUptimeKuma := middleware.APIKeyAuth(pgPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(uptimekumaHandler))
	mux.Handle("/api/v1/ingest/uptimekuma", protectedUptimeKuma)

	grafanaHandler := api.HandleGrafanaIngest(pgPool, redisClient)
	protectedGrafana := middleware.APIKeyAuth(pgPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(grafanaHandler))
	mux.Handle("/api/v1/ingest/grafana", protectedGrafana)

	zabbixHandler := api.HandleZabbixIngest(pgPool, redisClient)
	protectedZabbix := middleware.APIKeyAuth(pgPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(zabbixHandler))
	mux.Handle("/api/v1/ingest/zabbix", protectedZabbix)

	// Ingestion webhook endpoint (POST /api/v1/webhook/{integration_type}/{tenant_id})
	webhookHandler := api.HandleGenericWebhook(pgPool, redisClient)
	protectedWebhook := middleware.RateLimiter(redisClient, 500)(webhookHandler)
	mux.Handle("/api/v1/webhook/", protectedWebhook)

	// User authentication endpoints (unauthenticated)
	mux.Handle("/api/v1/auth/register", api.HandleRegister(pgPool))
	mux.Handle("/api/v1/auth/verify", api.HandleVerify(pgPool))
	mux.Handle("/api/v1/auth/login", api.HandleLogin(pgPool, jwtSecret))
	mux.Handle("/api/v1/public/tenants", api.HandleGetPublicTenants(pgPool))
	mux.Handle("/api/v1/tenants/update_style", middleware.JWTAuth(jwtSecret)(api.HandleUpdateTenantStyle(pgPool)))

	// Administrator endpoints (protected by JWT and Admin Role check)
	protectedAdminUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleAdminCreateUser(pgPool),
		),
	)
	protectedGetAdminUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleGetUsers(pgPool),
		),
	)
	protectedDeleteAdminUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleDeleteUser(pgPool),
		),
	)
	mux.Handle("/api/v1/admin/users", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			protectedAdminUsers.ServeHTTP(w, r)
		} else if r.Method == http.MethodGet {
			protectedGetAdminUsers.ServeHTTP(w, r)
		} else if r.Method == http.MethodDelete {
			protectedDeleteAdminUsers.ServeHTTP(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// SLA PDF Report Download Endpoint (Resolves auth token via URL query parameter for browser compatibility)
	mux.Handle("/api/v1/reports/sla", api.HandleDownloadSLAReport(pgPool, jwtSecret))
	mux.Handle("/api/v1/reports/sla/debug", api.HandleSLADebug(pgPool))

	// Secure Vault Credentials Storage Endpoint (Postgres Vault with RLS & GCM Ciphers, protected by JWT & Admin Role check)
	vaultRepo := repository.NewPostgresVaultRepository()
	protectedVault := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleSaveSecret(pgPool, vaultRepo),
		),
	)
	mux.Handle("/api/v1/vault/secret", protectedVault)

	// Tenant management routes (GET for listing active tenants, POST for admin-only creation)
	protectedGetTenants := middleware.JWTAuth(jwtSecret)(api.HandleGetTenants(pgPool))
	protectedPostTenants := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleCreateTenant(pgPool),
		),
	)
	protectedDeleteTenant := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleDeleteTenant(pgPool),
		),
	)
	mux.Handle("/api/v1/tenants", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			protectedPostTenants.ServeHTTP(w, r)
		} else if r.Method == http.MethodGet {
			protectedGetTenants.ServeHTTP(w, r)
		} else if r.Method == http.MethodDelete {
			protectedDeleteTenant.ServeHTTP(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// Integration management routes (GET to list active tenant integrations, POST to add, DELETE to remove)
	protectedGetIntegrations := middleware.JWTAuth(jwtSecret)(api.HandleGetIntegrations(pgPool))
	protectedPostIntegrations := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleCreateIntegration(pgPool),
		),
	)
	protectedDeleteIntegrations := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleDeleteIntegration(pgPool),
		),
	)
	mux.Handle("/api/v1/integrations", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			protectedGetIntegrations.ServeHTTP(w, r)
		} else if r.Method == http.MethodPost {
			protectedPostIntegrations.ServeHTTP(w, r)
		} else if r.Method == http.MethodDelete {
			protectedDeleteIntegrations.ServeHTTP(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// Incident action endpoints (Acknowledge and Resolve)
	protectedAcknowledgeIncident := middleware.JWTAuth(jwtSecret)(api.HandleAcknowledgeIncident(pgPool))
	protectedResolveIncident := middleware.JWTAuth(jwtSecret)(api.HandleResolveIncident(pgPool))
	mux.Handle("/api/v1/incidents/acknowledge", protectedAcknowledgeIncident)
	mux.Handle("/api/v1/incidents/resolve", protectedResolveIncident)

	// SLA dynamic report endpoint
	protectedGetSLAReport := middleware.JWTAuth(jwtSecret)(api.HandleGetSLAReport(pgPool))
	mux.Handle("/api/v1/reports/sla/stats", protectedGetSLAReport)

	// Runbook management and execution routes
	protectedGetRunbooks := middleware.JWTAuth(jwtSecret)(api.HandleGetRunbooks(pgPool))
	protectedPostRunbooks := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleCreateRunbook(pgPool),
		),
	)
	protectedExecuteRunbook := middleware.JWTAuth(jwtSecret)(api.HandleExecuteRunbook(pgPool))

	mux.Handle("/api/v1/runbooks", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			protectedGetRunbooks.ServeHTTP(w, r)
		} else if r.Method == http.MethodPost {
			protectedPostRunbooks.ServeHTTP(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/v1/runbooks/execute", protectedExecuteRunbook)

	// Incident chat & timeline endpoints
	protectedIncidentChat := middleware.JWTAuth(jwtSecret)(api.HandleIncidentChat(pgPool))
	protectedIncidentComments := middleware.JWTAuth(jwtSecret)(api.HandleGetIncidentComments(pgPool))
	mux.Handle("/api/v1/incidents/chat", protectedIncidentChat)
	mux.Handle("/api/v1/incidents/comments", protectedIncidentComments)

	// Runbooks execution audit logs endpoint
	protectedRunbookAudit := middleware.JWTAuth(jwtSecret)(api.HandleGetRunbookAuditLogs(pgPool))
	mux.Handle("/api/v1/runbooks/audit", protectedRunbookAudit)

	// Secure Vault metadata list endpoint
	protectedVaultList := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleGetVaultSecrets(pgPool),
		),
	)
	mux.Handle("/api/v1/vault/list", protectedVaultList)

	// ITSM Ticket Synchronization simulator endpoint
	protectedITSMSync := middleware.JWTAuth(jwtSecret)(api.HandleSyncITSM(pgPool))
	mux.Handle("/api/v1/itsm/sync", protectedITSMSync)

	// Shift Handover Endpoints
	protectedCreateHandover := middleware.JWTAuth(jwtSecret)(api.HandleCreateShiftHandover(pgPool))
	protectedGetCurrentHandover := middleware.JWTAuth(jwtSecret)(api.HandleGetCurrentShiftHandover(pgPool))
	protectedAckHandover := middleware.JWTAuth(jwtSecret)(api.HandleAcknowledgeShiftHandover(pgPool))
	
	mux.Handle("/api/v1/shift/handover", protectedCreateHandover)
	mux.Handle("/api/v1/shift/handover/current", protectedGetCurrentHandover)
	mux.Handle("/api/v1/shift/handover/ack", protectedAckHandover)

	// Real-Time Operator WebSocket Subscription endpoint (Multiplexed, resolved by JWT/APIKey/UUID)
	mux.Handle("/api/v1/ws", ws.ServeWS(hub, pgPool, jwtSecret))

	// Active operator sessions endpoint (Admin only)
	protectedActiveUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleGetActiveUsers(hub),
		),
	)
	mux.Handle("/api/v1/ws/active_users", protectedActiveUsers)

	// 6. Define & Launch Server with Timeout Controls (SRE Best Practice)
	srv := &http.Server{
		Addr:         ":" + serverPort,
		Handler:      middleware.CORS(mux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("NOC HTTP Ingestion API starting on port %s", serverPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Fatal: HTTP Server crashed: %v", err)
		}
	}()

	// 7. Orchestrate Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit
	log.Println("Shutdown signal received. Commencing SRE graceful drain sequence...")

	// Cancel background context to trigger worker loops to begin closing
	cancel()

	// Stop accepting new HTTP requests with a 10s grace period
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP Server forced to close: %v", err)
	} else {
		log.Println("HTTP Ingestion API stopped taking new connections.")
	}

	log.Println("NOC Core Engine shutdown complete.")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
