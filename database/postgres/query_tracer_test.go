package postgres_test

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

	e := logs.All()[0]

	if e.Stack != "" {
		t.Errorf("entry has a stack trace (%d bytes), want none", len(e.Stack))
	}

	if e.Caller.Defined {
		t.Errorf("entry caller = %v, want none (it is always query_tracer.go)", e.Caller)
	}

	fields := e.ContextMap()
	if loc, _ := fields["location"].(string); !strings.Contains(loc, "query_tracer_test.go:") {
		t.Errorf("location = %q, want this test file (the code that ran the query)", loc)
	}

	if fn, _ := fields["function"].(string); !strings.HasSuffix(fn, "TestQueryTracer_FailedQueryLogsOriginWithoutStack") {
		t.Errorf("function = %q, want this test function", fn)
	}

	if fields["sql"] != "INSERT INTO users (email) VALUES ('jane@example.com')" || fields["error"] != "duplicate key" {
		t.Errorf("sql, error = %v, %v, want the bound SQL and the error", fields["sql"], fields["error"])
	}
}

func TestQueryTracer_SuccessfulQueryHasNoOrigin(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	qt := postgres.NewQueryTracerForTest(zap.New(core), 0, false)

	qt.Log(context.Background(), tracelog.LogLevelInfo, "Query", map[string]any{"sql": "SELECT 1", "time": time.Millisecond})

	if _, ok := logs.All()[0].ContextMap()["location"]; ok {
		t.Error("a successful query should not pay for locating its caller")
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
