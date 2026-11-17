package postgres

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
