package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey string

const tenantKey contextKey = "tenant_id"

// Queryer abstracts pgxpool.Pool and pgx.Tx so repositories can accept either.
// NOTE: Due to PostgreSQL RLS with SET LOCAL, all tenant-scoped reads and writes
// MUST run within a transaction. Doing so prevents connection contamination
// within the pgx pool.
type Queryer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WithTenantID returns a new context containing the tenant ID.
func WithTenantID(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID)
}

// TenantIDFromContext retrieves the tenant ID from the context.
func TenantIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	tenantID, ok := ctx.Value(tenantKey).(uuid.UUID)
	return tenantID, ok
}

// ExecuteInTenantTx executes the provided callback function within a PostgreSQL transaction.
// It automatically retrieves the tenant ID from the context, runs 'SET LOCAL app.current_tenant_id'
// to enforce Row Level Security, and handles commit/rollback safely.
func ExecuteInTenantTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tenantID, ok := TenantIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("tenant_id not found in context")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Safe rollback in case of panic or early exit
	defer func() {
		if err := tx.Rollback(ctx); err != nil && err != pgx.ErrTxClosed {
			// In production, log this internal error using a logger
			_ = err
		}
	}()

	// Set the tenant ID in the local transaction scope.
	// SET LOCAL ensures this variable resets as soon as the transaction completes (Commit/Rollback),
	// which prevents connection contamination when the connection returns to the pgx pool.
	setLocalSQL := fmt.Sprintf("SET LOCAL app.current_tenant_id = '%s'", tenantID.String())
	if _, err := tx.Exec(ctx, setLocalSQL); err != nil {
		return fmt.Errorf("failed to set tenant RLS context: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
