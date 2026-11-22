package postgres

// =============================================================================
// TEST HELPERS (no test scenarios)
// =============================================================================
//
// File: export_test.go
// Purpose: Re-export unexported constructors so the black-box tests (package postgres_test) can
//          build a query tracer, a DB around a fake pool, and a pool config without a database.
// Used by: query_tracer_test.go, tracer_test.go, options_test.go
//
// =============================================================================

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"go.uber.org/zap"
)

// NewQueryTracerForTest exposes newQueryTracer to the external test package, whose frames (unlike
// package postgres's own) count as application code.
func NewQueryTracerForTest(log *zap.Logger, slow time.Duration, inlineArgs bool) tracelog.Logger {
	return newQueryTracer(log, slow, inlineArgs)
}

// NewDBWithDecorateForTest exposes newDB so tests can wrap a fake pool the way New does.
func NewDBWithDecorateForTest(pool DBPool, decorate func(DBTX) DBTX) *DB {
	return newDB(pool, decorate)
}

// NewPoolConfigForTest exposes newPoolConfig so option handling is testable without a database.
func NewPoolConfigForTest(opts Options, log *zap.Logger) (*pgxpool.Config, error) {
	return newPoolConfig(opts, log)
}
