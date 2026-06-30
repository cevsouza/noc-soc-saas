package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"noc-api/internal/api"
	"noc-api/internal/connector"
	"noc-api/internal/db"
	"noc-api/internal/middleware"
	redisclient "noc-api/internal/redis"
	"noc-api/internal/worker"
	"noc-api/internal/ws"
)

func main() {
	log.Println("Initializing NOC SaaS Core Engine...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Load Configurations from Environment Variables
	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	dbCfg := db.Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     dbPort,
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		DBName:   getEnv("DB_NAME", "noc"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}

	redisPort, _ := strconv.Atoi(getEnv("REDIS_PORT", "6379"))
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	redisCfg := redisclient.Config{
		Host:     getEnv("REDIS_HOST", "localhost"),
		Port:     redisPort,
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       redisDB,
	}

	serverPort := getEnv("SERVER_PORT", "8080")
	numWorkers, _ := strconv.Atoi(getEnv("WORKER_POOL_SIZE", "10"))

	// 2. Initialize PostgreSQL Connection Pool
	pgPool, err := db.NewConnectionPool(ctx, dbCfg)
	if err != nil {
		log.Fatalf("Fatal: Database initialization failed: %v", err)
	}
	defer pgPool.Close()
	log.Println("PostgreSQL Connection Pool initialized successfully.")

	// 3. Initialize Redis Client
	redisClient, err := redisclient.NewRedisClient(ctx, redisCfg)
	if err != nil {
		log.Fatalf("Fatal: Redis initialization failed: %v", err)
	}
	defer redisClient.Close()
	log.Println("Redis Client initialized successfully.")

	// 4. Initialize & Start Concurrent Worker Pool
	wp := worker.NewWorkerPool(pgPool, redisClient, numWorkers)
	wp.Start(ctx)
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

	// Health Check endpoint (unauthenticated)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","uptime":"online"}`))
	})

	// High-Performance Ingestion endpoint (protected by API Key auth middleware)
	ingestHandler := api.HandleIngest(redisClient)
	protectedIngest := middleware.APIKeyAuth(pgPool, redisClient)(ingestHandler)
	mux.Handle("/api/v1/ingest", protectedIngest)

	// High-Performance Prometheus Alertmanager & Wazuh Webhook Ingestion
	promHandler := api.HandlePrometheusIngest(redisClient)
	protectedProm := middleware.APIKeyAuth(pgPool, redisClient)(promHandler)
	mux.Handle("/api/v1/ingest/prometheus", protectedProm)

	wazuhHandler := api.HandleWazuhIngest(redisClient)
	protectedWazuh := middleware.APIKeyAuth(pgPool, redisClient)(wazuhHandler)
	mux.Handle("/api/v1/ingest/wazuh", protectedWazuh)

	// SLA PDF Report Download Endpoint (Resolves auth token via URL query parameter for browser compatibility)
	mux.Handle("/api/v1/reports/sla", api.HandleDownloadSLAReport())

	// Real-Time Operator WebSocket Subscription endpoint (Multiplexed)
	mux.Handle("/api/v1/ws", ws.ServeWS(hub, pgPool))

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
