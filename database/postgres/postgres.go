// Package postgres provides PostgreSQL connection pool management, transactions, and query logging.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"go.uber.org/zap"
)

// DBTX is the query-execution interface shared by a pool and a transaction (compatible with sqlc).
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DBPool is a connection pool: a DBTX that can also begin transactions, be pinged, and be closed.
type DBPool interface {
	DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
	Ping(ctx context.Context) error
	Close()
}

// Options configures New. A zero pool setting keeps the value from the DSN (pool_max_conns, ...)
// or, failing that, pgxpool's default.
type Options struct {
	// Name identifies this database in logs (e.g. "primary"); it matters once an app has several.
	Name string

	// DSN is the PostgreSQL connection string (e.g. "postgres://user:pass@host:5432/db").
	DSN string

	// MaxConnection is the maximum number of pooled connections.
	MaxConnection int32

	// MinConnection is the minimum number of pooled connections kept open.
	MinConnection int32

	// MaxConnLifetime is the maximum age of a pooled connection before it is recycled.
	MaxConnLifetime time.Duration

	// MaxConnIdleTime is the maximum idle time before a pooled connection is closed.
	MaxConnIdleTime time.Duration

	// HealthCheckPeriod is how often the pool checks idle connections' health.
	HealthCheckPeriod time.Duration

	// QueryLogging enables structured SQL query logging via zap.
	QueryLogging bool

	// LogQueryArgs logs query arguments alongside SQL (local debug only).
	LogQueryArgs bool

	// SlowQueryThreshold logs queries slower than this as WARN (0 = disabled).
	SlowQueryThreshold time.Duration

	// Decorate optionally wraps every DBTX handed out by Executor (e.g. for tracing or metrics).
	Decorate func(DBTX) DBTX
}

// DB is a database handle supporting connection pooling and transactions.
type DB struct {
	pool     DBPool
	decorate func(DBTX) DBTX
	executor DBTX
}

// New creates a PostgreSQL connection pool from opts (call Ping to verify connectivity).
//
// Usage:
//
//	db, err := postgres.New(postgres.Options{DSN: dsn, MaxConnection: 20}, log)
//	if err != nil {
//	    return err
//	}
//	defer db.Close()
func New(opts Options, log *zap.Logger) (*DB, error) {
	poolConfig, err := newPoolConfig(opts, log)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	return newDB(pool, opts.Decorate), nil
}

// newPoolConfig parses opts.DSN and applies opts' non-zero pool settings and query tracer.
func newPoolConfig(opts Options, log *zap.Logger) (*pgxpool.Config, error) {
	poolConfig, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}

	if opts.MaxConnection > 0 {
		poolConfig.MaxConns = opts.MaxConnection
	}

	if opts.MinConnection > 0 {
		poolConfig.MinConns = opts.MinConnection
	}

	if opts.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = opts.MaxConnLifetime
	}

	if opts.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = opts.MaxConnIdleTime
	}

	if opts.HealthCheckPeriod > 0 {
		poolConfig.HealthCheckPeriod = opts.HealthCheckPeriod
	}

	if opts.QueryLogging && log != nil {
		poolConfig.ConnConfig.Tracer = &tracelog.TraceLog{
			Logger:   newQueryTracer(log, opts.SlowQueryThreshold, opts.LogQueryArgs),
			LogLevel: tracelog.LogLevelInfo,
		}
	}

	return poolConfig, nil
}

// FromPool builds a DB from an existing DBPool, without a Decorate wrapper.
func FromPool(pool DBPool) *DB {
	return newDB(pool, nil)
}

// newDB constructs a DB, pre-decorating the pool-level executor once if decorate is non-nil.
func newDB(pool DBPool, decorate func(DBTX) DBTX) *DB {
	var executor DBTX = pool
	if decorate != nil {
		executor = decorate(pool)
	}

	return &DB{pool: pool, decorate: decorate, executor: executor}
}

// Executor returns the active transaction DBTX from ctx if available, otherwise the pool executor.
//
// Usage:
//
//	queries := sqlc.New(db.Executor(ctx))
func (d *DB) Executor(ctx context.Context) DBTX {
	if tx, ok := d.getTx(ctx); ok {
		if d.decorate != nil {
			return d.decorate(tx)
		}

		return tx
	}

	return d.executor
}

// Ping verifies the database is reachable.
func (d *DB) Ping(ctx context.Context) error {
	if err := d.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: ping: %w", err)
	}

	return nil
}

// Close closes every pooled connection.
func (d *DB) Close() {
	d.pool.Close()
}
