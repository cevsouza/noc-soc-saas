package db

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationsAdvisoryLockKey is an arbitrary but stable session-level advisory lock key,
// derived the same way Postgres's own hashtext() would. Session-level (not
// pg_advisory_xact_lock) because the loop below opens/commits multiple transactions itself —
// a single acquire/release pair around the whole function needs to survive across them.
const migrationsAdvisoryLockKey = "hashtext('noc_migrations')"

// RunMigrations automatically checks and applies pending database schema migrations on startup.
// Guarded by a Postgres advisory lock so two instances booting concurrently (a rolling deploy,
// or future horizontal scaling) can't both race to apply the same not-yet-applied migration —
// without it, two processes could both pass the "already applied?" check for the same file
// before either commits, and attempt to run it twice concurrently (some early migrations, e.g.
// 000001_init_schema, use plain CREATE TABLE with no IF NOT EXISTS, so that race would surface
// as a real error, not a silent no-op).
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// pg_advisory_lock is a SESSION-level lock — it must be acquired and released on the exact
	// same physical connection, which pool.Exec does not guarantee across separate calls (the
	// pool may hand out a different connection each time). Acquire one connection up front and
	// use it for the entire critical section below, including the per-migration transactions.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire a connection for migrations: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock("+migrationsAdvisoryLockKey+")"); err != nil {
		return fmt.Errorf("failed to acquire migrations advisory lock: %w", err)
	}
	defer func() {
		// Explicit unlock before the connection is returned to the pool — pgxpool recycles
		// connections rather than always closing them, so a session-level lock left held
		// would otherwise leak into whichever caller acquires this connection next.
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock("+migrationsAdvisoryLockKey+")"); err != nil {
			log.Printf("[Migrations] Failed to release advisory lock: %v", err)
		}
	}()

	log.Println("Database migration manager initialized. Checking schema migrations...")

	// 1. Create migrations tracking metadata table if not present
	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to initialize schema_migrations tracking: %w", err)
	}

	// 2. Scan embedded SQL files
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read embedded migrations directory: %w", err)
	}

	var sqlFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			sqlFiles = append(sqlFiles, entry.Name())
		}
	}
	sort.Strings(sqlFiles)

	// 3. Execute each migration within a transactional boundaries if not already applied
	for _, fileName := range sqlFiles {
		var exists bool
		err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", fileName).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to query migration status for %s: %w", fileName, err)
		}

		if exists {
			log.Printf("Migration %s already applied.", fileName)
			continue
		}

		log.Printf("Applying database migration: %s", fileName)
		content, err := migrationFiles.ReadFile("migrations/" + fileName)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", fileName, err)
		}

		// Perform schema update inside a transaction to prevent partial migrations
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", fileName, err)
		}
		defer tx.Rollback(ctx)

		_, err = tx.Exec(ctx, string(content))
		if err != nil {
			return fmt.Errorf("failed to execute migration script %s: %w", fileName, err)
		}

		_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", fileName)
		if err != nil {
			return fmt.Errorf("failed to log migration %s metadata: %w", fileName, err)
		}

		err = tx.Commit(ctx)
		if err != nil {
			return fmt.Errorf("failed to commit migration transaction for %s: %w", fileName, err)
		}

		log.Printf("Migration %s successfully applied.", fileName)
	}

	log.Println("Database migrations check complete. All tables are up to date.")
	return nil
}
