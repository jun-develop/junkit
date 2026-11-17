package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/tracelog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/sqlfmt"
)

// queryTracer is a pgx tracelog.Logger that logs queries to zap and flags slow ones.
type queryTracer struct {
	log                *zap.Logger
	slowQueryThreshold time.Duration
	inlineArgs         bool
}

// newQueryTracer returns a queryTracer whose logger drops zap's caller and error stack trace: the
// caller is always this file, and the stack is mostly pgx/gin frames. A failed query logs the
// application code location that ran it instead (see sqlfmt.Origin).
func newQueryTracer(log *zap.Logger, slowQueryThreshold time.Duration, inlineArgs bool) *queryTracer {
	log = log.WithOptions(zap.WithCaller(false), zap.AddStacktrace(zapcore.FatalLevel))

	return &queryTracer{log: log, slowQueryThreshold: slowQueryThreshold, inlineArgs: inlineArgs}
}

// Log implements tracelog.Logger; it logs only "Query" events, raising slow ones to WARN.
func (t *queryTracer) Log(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]any) {
	if msg != "Query" {
		return
	}

	zapLevel := mapLogLevel(level)
	duration, _ := data["time"].(time.Duration)

	isSlow := t.slowQueryThreshold > 0 && duration > t.slowQueryThreshold
	if isSlow && zapLevel < zap.WarnLevel {
		zapLevel = zap.WarnLevel
	}

	if !t.log.Core().Enabled(zapLevel) {
		return
	}

	sql, _ := data["sql"].(string)
	rawArgs, _ := data["args"].([]any)
	info := sqlfmt.Parse(sql)

	finalSQL := info.SQL
	if t.inlineArgs {
		finalSQL = sqlfmt.Bind(info.SQL, rawArgs)
	}

	fields := make([]zap.Field, 0, 9)
	fields = append(fields,
		zap.String("trace_id", logger.GetTraceID(ctx)),
		zap.Duration("latency", duration),
		zap.String("query_name", info.Name),
		zap.String("operation", info.Operation),
		zap.String("sql", finalSQL),
		zap.Int("arg_count", len(rawArgs)),
	)

	if err, ok := data["err"].(error); ok {
		location, function := sqlfmt.Origin()
		fields = append(fields,
			zap.Error(err),
			zap.String("location", location),
			zap.String("function", function),
		)
	}

	event := "SQL_EXECUTION"
	if isSlow {
		event = "SLOW_QUERY_DETECTION"
	}

	t.log.Log(zapLevel, event, fields...)
}

// mapLogLevel converts a pgx tracelog level to the equivalent zap level.
func mapLogLevel(level tracelog.LogLevel) zapcore.Level {
	switch level {
	case tracelog.LogLevelError:
		return zap.ErrorLevel
	case tracelog.LogLevelWarn:
		return zap.WarnLevel
	case tracelog.LogLevelInfo:
		return zap.InfoLevel
	case tracelog.LogLevelDebug, tracelog.LogLevelTrace:
		return zap.DebugLevel
	default:
		return zap.InfoLevel
	}
}
