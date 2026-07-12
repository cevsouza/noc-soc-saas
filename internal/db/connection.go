package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

func (c Config) ConnString() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode)
}

func NewConnectionPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	connStr := cfg.ConnString()

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection config: %w", err)
	}

	// Optimize pool settings for Railway resource constraints
	poolCfg.MaxConns = 8
	poolCfg.MinConns = 2
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	return pool, nil
}

// SetupAppRuntimeRole rotates the password of the non-superuser "noc_app_runtime" role from
// the given secret. The role itself (CREATE ROLE + GRANTs) is provisioned by migration
// 000012_app_role_hardening, not here — migrations are versioned SQL and must never embed a
// live secret, so the password is instead synced at every boot from the APP_DB_PASSWORD
// environment variable. Returns an error if the role does not exist yet (e.g. migration 000012
// was skipped because the connected user lacks CREATEROLE).
func SetupAppRuntimeRole(ctx context.Context, adminPool *pgxpool.Pool, password string) error {
	var exists bool
	if err := adminPool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = 'noc_app_runtime')").Scan(&exists); err != nil {
		return fmt.Errorf("failed to check noc_app_runtime role existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("role noc_app_runtime does not exist (migration 000012 may have been skipped due to insufficient privilege)")
	}

	escaped := strings.ReplaceAll(password, "'", "''")
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("ALTER ROLE noc_app_runtime WITH PASSWORD '%s'", escaped)); err != nil {
		return fmt.Errorf("failed to set noc_app_runtime password: %w", err)
	}
	return nil
}

// NewAppRolePool builds a connection pool authenticated as the non-superuser runtime role,
// reusing the same host/port/database/sslmode as the admin connection but overriding the
// user/password. adminDatabaseURL takes precedence if non-empty; otherwise fallback is used.
func NewAppRolePool(ctx context.Context, adminDatabaseURL string, fallback Config, username, password string) (*pgxpool.Pool, error) {
	var poolCfg *pgxpool.Config
	var err error

	if adminDatabaseURL != "" {
		poolCfg, err = pgxpool.ParseConfig(adminDatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DATABASE_URL for app role pool: %w", err)
		}
	} else {
		poolCfg, err = pgxpool.ParseConfig(fallback.ConnString())
		if err != nil {
			return nil, fmt.Errorf("failed to parse fallback config for app role pool: %w", err)
		}
	}

	poolCfg.ConnConfig.User = username
	poolCfg.ConnConfig.Password = password
	poolCfg.MaxConns = 8
	poolCfg.MinConns = 2
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create app role connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database as noc_app_runtime: %w", err)
	}

	return pool, nil
}
