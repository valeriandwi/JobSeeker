// Package storage owns the PostgreSQL connection pool and transaction helpers.
// All SQL in this project is parameterized: callers never build SQL from input.
package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgx pool with the helpers the rest of the app uses.
type DB struct {
	Pool *pgxpool.Pool
}

// Open connects to the database, verifying connectivity with a ping.
func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close releases all pooled connections.
func (d *DB) Close() { d.Pool.Close() }

// Ping verifies the database is reachable (used by /readyz).
func (d *DB) Ping(ctx context.Context) error { return d.Pool.Ping(ctx) }

// WithTx runs fn inside a transaction, committing on success and rolling back
// on error or panic. Nested calls reuse the outer transaction.
func (d *DB) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	if tx, ok := TxFromContext(ctx); ok {
		return fn(tx)
	}
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

type txKey struct{}

// WithTxContext stores a transaction on the context for nested helpers.
func WithTxContext(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFromContext extracts a transaction stored by WithTxContext.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, letting repositories
// run inside or outside a transaction without branching.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
