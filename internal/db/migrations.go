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

// RunMigrations automatically checks and applies pending database schema migrations on startup.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	log.Println("Database migration manager initialized. Checking schema migrations...")

	// 1. Create migrations tracking metadata table if not present
	_, err := pool.Exec(ctx, `
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
		err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", fileName).Scan(&exists)
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
		tx, err := pool.Begin(ctx)
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
