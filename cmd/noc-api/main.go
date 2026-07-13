package main

import (
	"context"
	"encoding/json"
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
	"noc-api/internal/security"
	"noc-api/internal/worker"
	"noc-api/internal/ws"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

func main() {
	log.Println("Initializing NOC SaaS Core Engine (Resilience Mode Active)...")

	// SECURITY: no fallback secret. A guessable/committed JWT secret lets anyone forge
	// admin-level tokens for any tenant, so we refuse to boot without a real one.
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) < 32 {
		log.Fatalf("Fatal: JWT_SECRET environment variable must be set to a value of at least 32 bytes before boot.")
	}

	// SECURITY: fail fast if the vault master key is missing, instead of only failing the
	// first time a request tries to encrypt/decrypt a tenant secret.
	if _, err := security.GetMasterKey(); err != nil {
		log.Fatalf("Fatal: %v (VAULT_MASTER_KEY must be a 32-byte value set before boot).", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Load PostgreSQL Connection (Support direct DATABASE_URL or fallback to parameters)
	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	fallbackDBCfg := db.Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     dbPort,
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		DBName:   getEnv("DB_NAME", "noc"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}

	var pgPool *pgxpool.Pool
	var err error
	databaseURL := getEnv("DATABASE_URL", "")

	if databaseURL != "" {
		log.Println("DATABASE_URL detected. Connecting to PostgreSQL using direct connection string...")
		poolCfg, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			log.Fatalf("Fatal: Failed to parse DATABASE_URL: %v", err)
		}
		// Optimize pool settings for Railway resource constraints
		poolCfg.MaxConns = 8
		poolCfg.MinConns = 2
		poolCfg.MaxConnIdleTime = 5 * time.Minute
		poolCfg.MaxConnLifetime = 30 * time.Minute
		poolCfg.HealthCheckPeriod = 30 * time.Second

		pgPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			log.Fatalf("Fatal: Failed to create connection pool from DATABASE_URL: %v", err)
		}
	} else {
		log.Println("No DATABASE_URL detected. Falling back to individual DB_HOST variables...")
		pgPool, err = db.NewConnectionPool(ctx, fallbackDBCfg)
		if err != nil {
			log.Fatalf("Fatal: Database initialization failed: %v", err)
		}
	}
	defer pgPool.Close()

	// pgPool above is the "admin" pool (table owner — runs migrations, can bypass RLS).
	// appPool is what all request handlers/workers actually use for tenant-scoped queries.
	// It defaults to the same admin pool, but if APP_DB_PASSWORD is configured we switch to
	// the non-superuser "noc_app_runtime" role (see migration 000012 + db.SetupAppRuntimeRole),
	// so FORCE ROW LEVEL SECURITY is actually meaningful rather than bypassed by table ownership.
	appPool := pgPool
	appDBPassword := os.Getenv("APP_DB_PASSWORD")
	dbRoleSeparationRequired := getEnv("DB_ROLE_SEPARATION_REQUIRED", "false") == "true"

	if appDBPassword != "" {
		log.Println("[DB ROLE SEPARATION] APP_DB_PASSWORD detected — running migrations and role setup synchronously before continuing boot...")
		setupCtx, cancelSetup := context.WithTimeout(ctx, 60*time.Second)
		if err := pgPool.Ping(setupCtx); err != nil {
			log.Fatalf("Fatal: could not reach PostgreSQL to configure role separation: %v", err)
		}
		if err := db.RunMigrations(setupCtx, pgPool); err != nil {
			log.Fatalf("Fatal: migrations failed while configuring role separation: %v", err)
		}
		if err := db.SetupAppRuntimeRole(setupCtx, pgPool, appDBPassword); err != nil {
			if dbRoleSeparationRequired {
				log.Fatalf("Fatal: DB_ROLE_SEPARATION_REQUIRED=true but role separation setup failed: %v", err)
			}
			log.Printf("[DB ROLE SEPARATION WARNING] %v — continuing with single-pool mode; tenant isolation depends entirely on explicit tenant_id filters in application code.", err)
		} else {
			newAppPool, err := db.NewAppRolePool(setupCtx, databaseURL, fallbackDBCfg, "noc_app_runtime", appDBPassword)
			if err != nil {
				if dbRoleSeparationRequired {
					log.Fatalf("Fatal: DB_ROLE_SEPARATION_REQUIRED=true but could not connect as noc_app_runtime: %v", err)
				}
				log.Printf("[DB ROLE SEPARATION WARNING] Failed to connect as noc_app_runtime (%v). Continuing with single-pool mode.", err)
			} else {
				appPool = newAppPool
				log.Println("[DB ROLE SEPARATION] Runtime now using non-superuser role 'noc_app_runtime' for defense-in-depth RLS enforcement.")
			}
		}
		cancelSetup()
	} else {
		log.Println("[DB ROLE SEPARATION] APP_DB_PASSWORD not set — skipping second-layer role separation (isolation relies on FORCE ROW LEVEL SECURITY plus explicit tenant_id filters).")
	}
	defer appPool.Close()

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

	// Verify database and redis connectivity asynchronously in background to avoid port bind timeouts (SRE non-blocking boot)
	go func() {
		// A. Verify DB Connection in background
		var dbPingErr error
		for attempt := 1; attempt <= 10; attempt++ {
			log.Printf("Verifying PostgreSQL connection (attempt %d/10)...", attempt)
			if dbPingErr = pgPool.Ping(context.Background()); dbPingErr == nil {
				break
			}
			log.Printf("PostgreSQL ping failed (retrying in 3s): %v", dbPingErr)
			time.Sleep(3 * time.Second)
		}
		if dbPingErr != nil {
			log.Printf("[DATABASE WARN] Failed to ping PostgreSQL database after 10 attempts: %v. Database operations will degrade.", dbPingErr)
		} else {
			log.Println("PostgreSQL Connection Pool verified successfully.")
			// Run migrations
			if err := db.RunMigrations(context.Background(), pgPool); err != nil {
				log.Printf("[DATABASE WARN] Database migration failed: %v", err)
			}
			// Ensure the current+next month's alerts partitions exist (see migration 000015) —
			// the range-partitioned table has no auto-creation otherwise, so this must run on
			// every boot, not just once at migration time.
			if err := db.EnsureAlertPartitions(context.Background(), pgPool); err != nil {
				log.Printf("[DATABASE WARN] Failed to ensure alerts partitions: %v", err)
			}
			// One-time startup fix: promote configured initial admin(s), if any.
			adminEmails := security.InitialAdminEmails()
			if len(adminEmails) > 0 {
				log.Println("[DATABASE INFO] Running background initial admin accounts verification fix...")
				fixCtx, cancelFix := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancelFix()
				_, errVerifyFix := pgPool.Exec(fixCtx, `
					UPDATE users
					SET is_verified = TRUE, global_role = 'admin'
					WHERE email = ANY($1)
				`, adminEmails)
				if errVerifyFix != nil {
					log.Printf("[DATABASE WARN] Failed to auto-verify initial admin accounts: %v", errVerifyFix)
				} else {
					log.Println("[DATABASE INFO] Initial admin accounts verified successfully in background.")
				}
			} else {
				log.Println("[DATABASE INFO] INITIAL_ADMIN_EMAILS not set — skipping automatic admin promotion.")
			}
		}

		// B. Verify Redis Connection in background
		var redisPingErr error
		for attempt := 1; attempt <= 10; attempt++ {
			log.Printf("Verifying Redis connection (attempt %d/10)...", attempt)
			if redisPingErr = redisClient.Ping(context.Background()).Err(); redisPingErr == nil {
				break
			}
			log.Printf("Redis ping failed (retrying in 3s): %v", redisPingErr)
			time.Sleep(3 * time.Second)
		}
		if redisPingErr != nil {
			log.Printf("[REDIS WARN] Failed to ping Redis server after 10 attempts: %v. Queue operations will degrade.", redisPingErr)
		} else {
			log.Println("Redis Client verified successfully.")
		}
	}()

	serverPort := getEnv("PORT", getEnv("SERVER_PORT", "8080"))
	numWorkers, _ := strconv.Atoi(getEnv("WORKER_POOL_SIZE", "10"))

	// 4. Initialize & Start Concurrent Worker Pool
	wp := worker.NewWorkerPool(appPool, redisClient, numWorkers)
	wp.Start(ctx)
	wp.StartWatchdog(ctx)
	wp.StartMappingEngine(ctx)
	defer wp.Stop()

	// 5. Initialize & Start WebSocket Infrastructure (SRE Multiplexed Pattern)
	hub := ws.NewHub()
	go hub.Run(ctx)
	go ws.StartGlobalPubSubMultiplexer(ctx, redisClient, hub)

	// 5.5 Start Microsoft Sentinel Background Connector
	sentinelConn := connector.NewSentinelConnector(appPool, redisClient)
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

	// Health Check endpoint (unauthenticated) — actually pings the DB and Redis rather than
	// returning a static string, so restart/alerting policies relying on this are meaningful.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		checkCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		dbStatus := "ok"
		if err := appPool.Ping(checkCtx); err != nil {
			dbStatus = "unreachable"
		}

		redisStatus := "ok"
		if err := redisClient.Ping(checkCtx).Err(); err != nil {
			redisStatus = "unreachable"
		}

		healthy := dbStatus == "ok" && redisStatus == "ok"
		status := "healthy"
		statusCode := http.StatusOK
		if !healthy {
			status = "unhealthy"
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": status,
			"checks": map[string]string{
				"database": dbStatus,
				"redis":    redisStatus,
			},
		})
	})

	// Prometheus metrics endpoint — only responds if METRICS_TOKEN is configured AND the
	// request supplies that exact value (header or query param); otherwise 404, so nothing is
	// exposed on this public Railway URL by default.
	metricsToken := os.Getenv("METRICS_TOKEN")
	promMetricsHandler := promhttp.Handler()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("X-Metrics-Token")
		if supplied == "" {
			supplied = r.URL.Query().Get("token")
		}
		if metricsToken == "" || supplied != metricsToken {
			http.NotFound(w, r)
			return
		}
		promMetricsHandler.ServeHTTP(w, r)
	})

	// High-Performance Ingestion endpoint (protected by API Key auth middleware & rate limiter)
	ingestHandler := api.HandleIngest(appPool, redisClient)
	protectedIngest := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(ingestHandler))
	mux.Handle("/api/v1/ingest", protectedIngest)

	// High-Performance Prometheus Alertmanager & Wazuh Webhook Ingestion
	promHandler := api.HandlePrometheusIngest(appPool, redisClient)
	protectedProm := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(promHandler))
	mux.Handle("/api/v1/ingest/prometheus", protectedProm)

	wazuhHandler := api.HandleWazuhIngest(appPool, redisClient)
	protectedWazuh := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(wazuhHandler))
	mux.Handle("/api/v1/ingest/wazuh", protectedWazuh)

	// High-Performance Uptime Kuma, Grafana & Zabbix Webhook Ingestions
	uptimekumaHandler := api.HandleUptimeKumaIngest(appPool, redisClient)
	protectedUptimeKuma := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(uptimekumaHandler))
	mux.Handle("/api/v1/ingest/uptimekuma", protectedUptimeKuma)

	grafanaHandler := api.HandleGrafanaIngest(appPool, redisClient)
	protectedGrafana := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(grafanaHandler))
	mux.Handle("/api/v1/ingest/grafana", protectedGrafana)

	zabbixHandler := api.HandleZabbixIngest(appPool, redisClient)
	protectedZabbix := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(zabbixHandler))
	mux.Handle("/api/v1/ingest/zabbix", protectedZabbix)

	// OTLP/HTTP+JSON logs, Icinga/Nagios, Azure Monitor, PagerDuty, Opsgenie (inbound), and
	// CloudWatch (via SNS) ingestion — same auth/rate-limit wrapping as the connectors above.
	otlpHandler := api.HandleOTLPIngest(appPool, redisClient)
	protectedOTLP := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(otlpHandler))
	mux.Handle("/api/v1/ingest/otlp", protectedOTLP)

	icingaHandler := api.HandleIcingaIngest(appPool, redisClient)
	protectedIcinga := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(icingaHandler))
	mux.Handle("/api/v1/ingest/icinga", protectedIcinga)

	azureMonitorHandler := api.HandleAzureMonitorIngest(appPool, redisClient)
	protectedAzureMonitor := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(azureMonitorHandler))
	mux.Handle("/api/v1/ingest/azuremonitor", protectedAzureMonitor)

	pagerDutyHandler := api.HandlePagerDutyIngest(appPool, redisClient)
	protectedPagerDuty := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(pagerDutyHandler))
	mux.Handle("/api/v1/ingest/pagerduty", protectedPagerDuty)

	opsgenieHandler := api.HandleOpsgenieIngest(appPool, redisClient)
	protectedOpsgenie := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(opsgenieHandler))
	mux.Handle("/api/v1/ingest/opsgenie", protectedOpsgenie)

	cloudwatchHandler := api.HandleCloudWatchIngest(appPool, redisClient)
	protectedCloudWatch := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(cloudwatchHandler))
	mux.Handle("/api/v1/ingest/cloudwatch", protectedCloudWatch)

	// EDR (CrowdStrike) and firewall (Palo Alto, Fortinet) inbound connectors — same auth/rate-limit
	// wrapping as the connectors above.
	crowdstrikeHandler := api.HandleCrowdStrikeIngest(appPool, redisClient)
	protectedCrowdStrike := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(crowdstrikeHandler))
	mux.Handle("/api/v1/ingest/crowdstrike", protectedCrowdStrike)

	paloAltoHandler := api.HandlePaloAltoIngest(appPool, redisClient)
	protectedPaloAlto := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(paloAltoHandler))
	mux.Handle("/api/v1/ingest/paloalto", protectedPaloAlto)

	fortinetHandler := api.HandleFortinetIngest(appPool, redisClient)
	protectedFortinet := middleware.APIKeyAuth(appPool, redisClient, jwtSecret)(middleware.RateLimiter(redisClient, 500)(fortinetHandler))
	mux.Handle("/api/v1/ingest/fortinet", protectedFortinet)

	// Secure Vault Credentials Storage Endpoint (Postgres Vault with RLS & GCM Ciphers, protected by JWT & Admin Role check)
	vaultRepo := repository.NewPostgresVaultRepository()

	// Ingestion webhook endpoint (POST /api/v1/webhook/{integration_type}/{tenant_id}) — requires
	// a valid X-Signature HMAC keyed with the tenant's own webhook_hmac_secret (see /webhook-secret below).
	webhookHandler := api.HandleGenericWebhook(appPool, redisClient, vaultRepo)
	protectedWebhook := middleware.RateLimiter(redisClient, 500)(webhookHandler)
	mux.Handle("/api/v1/webhook/", protectedWebhook)

	// User authentication endpoints (unauthenticated)
	mux.Handle("/api/v1/auth/register", api.HandleRegister(appPool))
	mux.Handle("/api/v1/auth/verify", api.HandleVerify(appPool))
	mux.Handle("/api/v1/auth/login", api.HandleLogin(appPool, jwtSecret))
	mux.Handle("/api/v1/public/tenants", api.HandleGetPublicTenants(appPool))
	mux.Handle("/api/v1/tenants/update_style", middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleUpdateTenantStyle(appPool),
		),
	))

	// Administrator endpoints (these act on the platform's entire user base with no tenant
	// scoping at all — "Users are global" per HandleAdminCreateUser — so they require
	// GlobalRole==admin, not just Role==admin in the caller's own tenant).
	protectedAdminUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleAdminCreateUser(appPool),
		),
	)
	protectedGetAdminUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleGetUsers(appPool),
		),
	)
	protectedDeleteAdminUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleDeleteUser(appPool),
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

	// Tenant access-grant management (Fase 5 fatia 1): an admin authorizes an operator on
	// specific tenants, one by one, populating tenant_users (the table every tenant-scope
	// authorization check already consumes). Platform-level, so GlobalRole==admin only.
	protectedGetAccess := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleGetUserAccess(appPool),
		),
	)
	protectedGrantAccess := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleGrantUserAccess(appPool),
		),
	)
	protectedRevokeAccess := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleRevokeUserAccess(appPool),
		),
	)
	mux.Handle("/api/v1/admin/access", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			protectedGetAccess.ServeHTTP(w, r)
		case http.MethodPost:
			protectedGrantAccess.ServeHTTP(w, r)
		case http.MethodDelete:
			protectedRevokeAccess.ServeHTTP(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// SLA PDF Report Download Endpoint (resolves auth via ?token= — kept unauthenticated at the
	// route level since browser downloads can't set an Authorization header; security instead
	// comes from ResolveTenantFromToken now only accepting a signed JWT or a real API key, never
	// a raw tenant UUID)
	mux.Handle("/api/v1/reports/sla", api.HandleDownloadSLAReport(appPool, jwtSecret))
	mux.Handle("/api/v1/reports/sla/debug", api.HandleSLADebug(appPool))

	protectedVault := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleSaveSecret(appPool, vaultRepo),
		),
	)
	mux.Handle("/api/v1/vault/secret", protectedVault)

	// Per-tenant webhook signing secret provisioning (admin only)
	protectedWebhookSecret := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleGenerateWebhookSecret(appPool, vaultRepo),
		),
	)
	mux.Handle("/api/v1/integrations/webhook-secret", protectedWebhookSecret)

	// Tenant management routes (GET for listing active tenants, POST/DELETE are platform-wide
	// actions not scoped to the caller's own tenant, so they require GlobalRole==admin —
	// a tenant-level admin (Role==admin) must NOT be able to create or delete other tenants).
	protectedGetTenants := middleware.JWTAuth(jwtSecret)(api.HandleGetTenants(appPool))
	protectedPostTenants := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleCreateTenant(appPool),
		),
	)
	protectedDeleteTenant := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleDeleteTenant(appPool),
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
	protectedGetIntegrations := middleware.JWTAuth(jwtSecret)(api.HandleGetIntegrations(appPool))
	protectedPostIntegrations := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleCreateIntegration(appPool),
		),
	)
	protectedDeleteIntegrations := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleDeleteIntegration(appPool),
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

	protectedGetIntegrationStatus := middleware.JWTAuth(jwtSecret)(api.HandleGetIntegrationStatus(appPool, redisClient))
	mux.Handle("/api/v1/integrations/status", protectedGetIntegrationStatus)

	// Alerts list endpoint
	alertRepo := repository.NewPostgresAlertRepository()
	protectedListAlerts := middleware.JWTAuth(jwtSecret)(api.HandleListAlerts(appPool, alertRepo))
	mux.Handle("/api/v1/alerts", protectedListAlerts)

	protectedCleanupAlerts := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleCleanupAlerts(appPool),
		),
	)
	mux.Handle("/api/v1/alerts/cleanup", protectedCleanupAlerts)

	// Incident action endpoints (Acknowledge and Resolve)
	protectedAcknowledgeIncident := middleware.JWTAuth(jwtSecret)(api.HandleAcknowledgeIncident(appPool))
	protectedResolveIncident := middleware.JWTAuth(jwtSecret)(api.HandleResolveIncident(appPool))
	mux.Handle("/api/v1/incidents/acknowledge", protectedAcknowledgeIncident)
	mux.Handle("/api/v1/incidents/resolve", protectedResolveIncident)

	// SLA dynamic report endpoint
	protectedGetSLAReport := middleware.JWTAuth(jwtSecret)(api.HandleGetSLAReport(appPool))
	mux.Handle("/api/v1/reports/sla/stats", protectedGetSLAReport)

	// Per-tenant SLA config (Fase 3): GET the effective targets (defaults + overrides); PUT to
	// customize, gated to tenant admins. The SLA report reads these targets instead of a hardcode.
	getSLAConfig := api.HandleGetSLAConfig(appPool)
	setSLAConfig := middleware.RequireRole(model.RoleTenantAdmin)(api.HandleSetSLAConfig(appPool))
	mux.Handle("/api/v1/reports/sla/config", middleware.JWTAuth(jwtSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			setSLAConfig.ServeHTTP(w, r)
			return
		}
		getSLAConfig.ServeHTTP(w, r)
	})))

	// Operational KPI bundle (Fase 6 fatia 1): tactical NOC/SOC metrics (triage backlog, alert
	// noise ratio, top offenders, automation ROI, MITRE breakdown, silent telemetry sources) that
	// complement the SLA executive report. Same authenticated-user access level as SLA stats.
	protectedGetOperationalStats := middleware.JWTAuth(jwtSecret)(api.HandleGetOperationalStats(appPool, redisClient))
	mux.Handle("/api/v1/reports/operational/stats", protectedGetOperationalStats)

	// Real asset topology: the tenant's actual reporting hosts derived from its alert stream,
	// replacing the old hardcoded 6-node SVG. Same authenticated-user access level as SLA stats.
	protectedGetTopology := middleware.JWTAuth(jwtSecret)(api.HandleGetTopology(appPool))
	mux.Handle("/api/v1/topology", protectedGetTopology)

	// Global search (alerts/runbooks/tenants), scoped to whatever tenants the caller has access to
	protectedSearch := middleware.JWTAuth(jwtSecret)(api.HandleGlobalSearch(appPool))
	mux.Handle("/api/v1/search", protectedSearch)

	// Runbook management and execution routes
	protectedGetRunbooks := middleware.JWTAuth(jwtSecret)(api.HandleGetRunbooks(appPool))
	protectedPostRunbooks := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleCreateRunbook(appPool),
		),
	)
	protectedDeleteRunbooks := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleDeleteRunbook(appPool),
		),
	)
	protectedExecuteRunbook := middleware.JWTAuth(jwtSecret)(api.HandleExecuteRunbook(appPool))

	mux.Handle("/api/v1/runbooks", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			protectedGetRunbooks.ServeHTTP(w, r)
		} else if r.Method == http.MethodPost {
			protectedPostRunbooks.ServeHTTP(w, r)
		} else if r.Method == http.MethodDelete {
			protectedDeleteRunbooks.ServeHTTP(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	mux.Handle("/api/v1/runbooks/execute", protectedExecuteRunbook)

	// SOAR auto-trigger approval queue (operators/admins review and approve/reject)
	protectedGetApprovals := middleware.JWTAuth(jwtSecret)(api.HandleGetRunbookApprovals(appPool))
	protectedApproveRunbook := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin, model.RoleOperator)(
			api.HandleApproveRunbookRequest(appPool),
		),
	)
	protectedRejectRunbook := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin, model.RoleOperator)(
			api.HandleRejectRunbookRequest(appPool),
		),
	)
	mux.Handle("/api/v1/runbooks/approvals", protectedGetApprovals)
	mux.Handle("/api/v1/runbooks/approvals/approve", protectedApproveRunbook)
	mux.Handle("/api/v1/runbooks/approvals/reject", protectedRejectRunbook)

	// Outbound response actions (Fase 5 fatia 4): vendor-native containment — block/unblock a
	// source IP on a firewall (Palo Alto, Fortinet) or contain/lift a host on the EDR
	// (CrowdStrike). Every action mutates network/endpoint state, so it is filed as a request
	// and only executed on approval — same operator/admin approval gate as runbook auto-triggers.
	protectedListResponse := middleware.JWTAuth(jwtSecret)(api.HandleGetResponseActions(appPool))
	protectedCreateResponse := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin, model.RoleOperator)(
			api.HandleCreateResponseAction(appPool),
		),
	)
	protectedApproveResponse := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin, model.RoleOperator)(
			api.HandleApproveResponseAction(appPool),
		),
	)
	protectedRejectResponse := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin, model.RoleOperator)(
			api.HandleRejectResponseAction(appPool),
		),
	)
	mux.Handle("/api/v1/response/requests", protectedListResponse)
	mux.Handle("/api/v1/response/request", protectedCreateResponse)
	mux.Handle("/api/v1/response/approve", protectedApproveResponse)
	mux.Handle("/api/v1/response/reject", protectedRejectResponse)

	// Incident chat & timeline endpoints
	protectedIncidentChat := middleware.JWTAuth(jwtSecret)(api.HandleIncidentChat(appPool))
	protectedIncidentComments := middleware.JWTAuth(jwtSecret)(api.HandleGetIncidentComments(appPool))
	mux.Handle("/api/v1/incidents/chat", protectedIncidentChat)
	mux.Handle("/api/v1/incidents/comments", protectedIncidentComments)

	// Runbooks execution audit logs endpoint
	protectedRunbookAudit := middleware.JWTAuth(jwtSecret)(api.HandleGetRunbookAuditLogs(appPool))
	mux.Handle("/api/v1/runbooks/audit", protectedRunbookAudit)

	// Secure Vault metadata list endpoint
	protectedVaultList := middleware.JWTAuth(jwtSecret)(
		middleware.RequireRole(model.RoleAdmin)(
			api.HandleGetVaultSecrets(appPool),
		),
	)
	mux.Handle("/api/v1/vault/list", protectedVaultList)

	// ITSM Ticket Synchronization simulator endpoint
	protectedITSMSync := middleware.JWTAuth(jwtSecret)(api.HandleSyncITSM(appPool))
	mux.Handle("/api/v1/itsm/sync", protectedITSMSync)

	// Shift Handover Endpoints
	protectedCreateHandover := middleware.JWTAuth(jwtSecret)(api.HandleCreateShiftHandover(appPool))
	protectedGetCurrentHandover := middleware.JWTAuth(jwtSecret)(api.HandleGetCurrentShiftHandover(appPool))
	protectedAckHandover := middleware.JWTAuth(jwtSecret)(api.HandleAcknowledgeShiftHandover(appPool))

	mux.Handle("/api/v1/shift/handover", protectedCreateHandover)
	mux.Handle("/api/v1/shift/handover/current", protectedGetCurrentHandover)
	mux.Handle("/api/v1/shift/handover/ack", protectedAckHandover)

	// Real-Time Operator WebSocket Subscription endpoint (Multiplexed; requires a valid JWT —
	// see internal/ws/ws_handler.go for the tenant membership validation on ?tenants=)
	mux.Handle("/api/v1/ws", ws.ServeWS(hub, appPool, jwtSecret))

	// Active operator sessions endpoint — lists connected clients across ALL tenants with no
	// tenant filter, so it requires GlobalRole==admin rather than tenant-scoped Role==admin.
	protectedActiveUsers := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleGetActiveUsers(hub),
		),
	)

	// Dead-letter queue inspection/replay — platform-wide, not tenant-scoped, so it requires
	// GlobalRole==admin.
	protectedGetDLQ := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleGetDLQ(redisClient),
		),
	)
	protectedReplayDLQ := middleware.JWTAuth(jwtSecret)(
		middleware.RequireGlobalRole(model.RoleAdmin)(
			api.HandleReplayDLQ(redisClient),
		),
	)
	mux.Handle("/api/v1/dlq", protectedGetDLQ)
	mux.Handle("/api/v1/dlq/replay", protectedReplayDLQ)
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
