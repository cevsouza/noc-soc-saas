// cmd/migrate is a standalone tool that applies pending schema migrations and (optionally)
// provisions the noc_app_runtime role, without booting the full HTTP server/workers/Redis
// stack that cmd/noc-api needs. It exists so CI (and any one-off manual migration run) can
// prepare a database in a single short-lived process instead of running the entire API binary
// just to reach the migration step.
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"noc-api/internal/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dbPort, _ := strconv.Atoi(getEnv("DB_PORT", "5432"))
	fallbackDBCfg := db.Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     dbPort,
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		DBName:   getEnv("DB_NAME", "noc"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}

	var pool *pgxpool.Pool
	var err error
	databaseURL := os.Getenv("DATABASE_URL")

	if databaseURL != "" {
		var poolCfg *pgxpool.Config
		poolCfg, err = pgxpool.ParseConfig(databaseURL)
		if err != nil {
			log.Fatalf("Fatal: failed to parse DATABASE_URL: %v", err)
		}
		pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	} else {
		pool, err = db.NewConnectionPool(ctx, fallbackDBCfg)
	}
	if err != nil {
		log.Fatalf("Fatal: failed to create connection pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Fatal: could not reach PostgreSQL: %v", err)
	}

	log.Println("Running migrations...")
	if err := db.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Fatal: migrations failed: %v", err)
	}
	log.Println("Migrations applied successfully.")

	// Mirrors cmd/noc-api's boot-time role-separation setup (see main.go), so that a database
	// prepared by this tool ends up in the exact same state a real deploy would leave it in —
	// including the noc_app_runtime role being ready for callers to connect as.
	if appDBPassword := os.Getenv("APP_DB_PASSWORD"); appDBPassword != "" {
		log.Println("APP_DB_PASSWORD set — configuring noc_app_runtime role...")
		if err := db.SetupAppRuntimeRole(ctx, pool, appDBPassword); err != nil {
			log.Fatalf("Fatal: failed to configure noc_app_runtime role: %v", err)
		}
		log.Println("noc_app_runtime role configured.")
	}
}
