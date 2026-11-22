package postgres_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: query_tracer_test.go
// Purpose: Verify what the pgx query tracer writes to the zap log: level, event name, origin and SQL.
// Type: Unit (zap observer captures log entries)
//
// Test functions: 6
// Total scenarios: 13
//
// #   Test Case                                          Scenario                             Expected Result
// 1   TestQueryTracer_FailedQueryLogsOriginWithoutStack  Query fails                          One entry: no stack, no caller; origin is the test; bound SQL and error logged
// 2   TestQueryTracer_SuccessfulQueryHasNoOrigin         Query succeeds                       Entry has no location field
// 3   TestQueryTracer_ArgsInlinedOnlyWhenEnabled         Inlining disabled (default)          SQL keeps $1 placeholder; arg_count is 1
// 4   TestQueryTracer_ArgsInlinedOnlyWhenEnabled         Inlining enabled (LogQueryArgs)      SQL has the value inlined; arg_count is 1
// 5   TestQueryTracer_MapsPgxLevelToZapLevel             pgx Error                            zap Error
// 6   TestQueryTracer_MapsPgxLevelToZapLevel             pgx Warn                             zap Warn
// 7   TestQueryTracer_MapsPgxLevelToZapLevel             pgx Info                             zap Info
// 8   TestQueryTracer_MapsPgxLevelToZapLevel             pgx Debug                            zap Debug
// 9   TestQueryTracer_MapsPgxLevelToZapLevel             pgx Trace                            zap Debug
// 10  TestQueryTracer_MapsPgxLevelToZapLevel             pgx None (unknown)                   zap Info (default)
// 11  TestQueryTracer_FlagsSlowQueries                   Info query above the 10ms threshold  WARN "SLOW_QUERY_DETECTION"
// 12  TestQueryTracer_FlagsSlowQueries                   Info query below the threshold       INFO "SQL_EXECUTION"
// 13  TestQueryTracer_IgnoresNonQueryEvents              pgx "Connect" event                  Nothing logged
//
// =============================================================================

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/tracelog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/jun-develop/junkit/database/postgres"
)

func TestQueryTracer_FailedQueryLogsOriginWithoutStack(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	log := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	qt := postgres.NewQueryTracerForTest(log, 0, true)

	qt.Log(context.Background(), tracelog.LogLevelError, "Query", map[string]any{
		"sql":  "-- name: CreateUser :one\nINSERT INTO users (email) VALUES ($1)",
		"args": []any{"jane@example.com"},
		"time": time.Millisecond,
		"err":  errors.New("duplicate key"),
	})

	if logs.Len() != 1 {
		t.Fatalf("got %d log entries, want 1", logs.Len())
	}

	entry := logs.All()[0]
	fields := entry.ContextMap()

	if entry.Stack != "" {
		t.Errorf("entry has a stack trace (%d bytes), want none", len(entry.Stack))
	}

	if entry.Caller.Defined {
		t.Errorf("entry caller = %v, want none (it is always query_tracer.go)", entry.Caller)
	}

	if loc, _ := fields["location"].(string); !strings.Contains(loc, "query_tracer_test.go:") {
		t.Errorf("location = %q, want this test file (the code that ran the query)", loc)
	}

	if fn, _ := fields["function"].(string); !strings.HasSuffix(fn, "TestQueryTracer_FailedQueryLogsOriginWithoutStack") {
		t.Errorf("function = %q, want this test function", fn)
	}

	if want := "INSERT INTO users (email) VALUES ('jane@example.com')"; fields["sql"] != want {
		t.Errorf("sql = %v, want %q (bound SQL without the sqlc name comment)", fields["sql"], want)
	}

	if fields["error"] != "duplicate key" {
		t.Errorf("error = %v, want %q", fields["error"], "duplicate key")
	}
}

func TestQueryTracer_SuccessfulQueryHasNoOrigin(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	qt := postgres.NewQueryTracerForTest(zap.New(core), 0, false)

	qt.Log(context.Background(), tracelog.LogLevelInfo, "Query", map[string]any{"sql": "SELECT 1", "time": time.Millisecond})

	if logs.Len() != 1 {
		t.Fatalf("got %d log entries, want 1", logs.Len())
	}

	if _, ok := logs.All()[0].ContextMap()["location"]; ok {
		t.Error("entry has a location field, want none: a successful query should not pay for locating its caller")
	}
}

func TestQueryTracer_ArgsInlinedOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name       string
		inlineArgs bool
		wantSQL    string
	}{
		{name: "default keeps placeholders", inlineArgs: false, wantSQL: "SELECT * FROM users WHERE email = $1"},
		{name: "LogQueryArgs inlines values", inlineArgs: true, wantSQL: "SELECT * FROM users WHERE email = 'jane@example.com'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			qt := postgres.NewQueryTracerForTest(zap.New(core), 0, tt.inlineArgs)

			qt.Log(context.Background(), tracelog.LogLevelInfo, "Query", map[string]any{
				"sql":  "SELECT * FROM users WHERE email = $1",
				"args": []any{"jane@example.com"},
				"time": time.Millisecond,
			})

			if logs.Len() != 1 {
				t.Fatalf("got %d log entries, want 1", logs.Len())
			}

			fields := logs.All()[0].ContextMap()

			if fields["sql"] != tt.wantSQL {
				t.Errorf("sql = %v, want %q", fields["sql"], tt.wantSQL)
			}

			if fields["arg_count"] != int64(1) {
				t.Errorf("arg_count = %v, want 1", fields["arg_count"])
			}
		})
	}
}

func TestQueryTracer_MapsPgxLevelToZapLevel(t *testing.T) {
	tests := []struct {
		name  string
		level tracelog.LogLevel
		want  zapcore.Level
	}{
		{"error", tracelog.LogLevelError, zapcore.ErrorLevel},
		{"warn", tracelog.LogLevelWarn, zapcore.WarnLevel},
		{"info", tracelog.LogLevelInfo, zapcore.InfoLevel},
		{"debug", tracelog.LogLevelDebug, zapcore.DebugLevel},
		{"trace", tracelog.LogLevelTrace, zapcore.DebugLevel},
		{"none (unknown level)", tracelog.LogLevelNone, zapcore.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			qt := postgres.NewQueryTracerForTest(zap.New(core), 0, false)

			qt.Log(context.Background(), tt.level, "Query", map[string]any{"sql": "SELECT 1"})

			if logs.Len() != 1 {
				t.Fatalf("got %d log entries, want 1", logs.Len())
			}

			if got := logs.All()[0].Level; got != tt.want {
				t.Errorf("entry level = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueryTracer_FlagsSlowQueries(t *testing.T) {
	tests := []struct {
		name      string
		latency   time.Duration
		wantLevel zapcore.Level
		wantEvent string
	}{
		{"query slower than the threshold", 50 * time.Millisecond, zapcore.WarnLevel, "SLOW_QUERY_DETECTION"},
		{"query faster than the threshold", time.Millisecond, zapcore.InfoLevel, "SQL_EXECUTION"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			qt := postgres.NewQueryTracerForTest(zap.New(core), 10*time.Millisecond, false)

			qt.Log(context.Background(), tracelog.LogLevelInfo, "Query", map[string]any{
				"sql":  "SELECT 1",
				"time": tt.latency,
			})

			if logs.Len() != 1 {
				t.Fatalf("got %d log entries, want 1", logs.Len())
			}

			entry := logs.All()[0]

			if entry.Level != tt.wantLevel {
				t.Errorf("entry level = %v, want %v", entry.Level, tt.wantLevel)
			}

			if entry.Message != tt.wantEvent {
				t.Errorf("entry message = %q, want %q", entry.Message, tt.wantEvent)
			}
		})
	}
}

func TestQueryTracer_IgnoresNonQueryEvents(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	qt := postgres.NewQueryTracerForTest(zap.New(core), 0, false)

	qt.Log(context.Background(), tracelog.LogLevelInfo, "Connect", map[string]any{"sql": "SELECT 1"})

	if logs.Len() != 0 {
		t.Errorf("got %d log entries, want 0 for a non-Query event", logs.Len())
	}
}
