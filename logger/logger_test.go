package logger_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jun-develop/junkit/logger"
)

// fakeLifecycle is an fx.Lifecycle that records hooks so tests can run them without an Fx app.
type fakeLifecycle struct {
	hooks []fx.Hook
}

// Append records h.
func (f *fakeLifecycle) Append(h fx.Hook) {
	f.hooks = append(f.hooks, h)
}

func TestNew_DevelopmentLogsToStdoutOnly(t *testing.T) {
	lc := &fakeLifecycle{}
	log, err := logger.New(logger.Options{Environment: "development", Level: "debug"}, lc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if log == nil {
		t.Fatal("New() returned a nil logger")
	}

	if !log.Core().Enabled(zapcore.DebugLevel) {
		t.Error("debug level should be enabled when Level is \"debug\"")
	}

	if len(lc.hooks) != 1 {
		t.Fatalf("got %d lifecycle hooks, want 1 (OnStop for flushing)", len(lc.hooks))
	}

	if err := lc.hooks[0].OnStop(context.Background()); err != nil {
		t.Fatalf("OnStop() error = %v, want nil", err)
	}
}

func TestNew_NonDevelopmentWritesToFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "app.log")
	lc := &fakeLifecycle{}

	log, err := logger.New(logger.Options{
		Environment: "production",
		Level:       "info",
		Filename:    logPath,
		MaxSize:     1,
	}, lc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	log.Info("hello from the test")

	if err := lc.hooks[0].OnStop(context.Background()); err != nil {
		t.Fatalf("OnStop() error = %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected a log file at %s: %v", logPath, err)
	}

	if len(data) == 0 {
		t.Fatal("log file is empty, want the logged line written to it")
	}
}

func TestNew_OutputFileOverridesDevelopment(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "app.log")
	lc := &fakeLifecycle{}

	log, err := logger.New(logger.Options{
		Environment: "development",
		Output:      logger.OutputFile,
		Level:       "info",
		Filename:    logPath,
		MaxSize:     1,
	}, lc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	log.Info("hello from the test")

	if err := lc.hooks[0].OnStop(context.Background()); err != nil {
		t.Fatalf("OnStop() error = %v", err)
	}

	if data, err := os.ReadFile(logPath); err != nil || len(data) == 0 {
		t.Fatalf("want the line written to %s, got err=%v len=%d", logPath, err, len(data))
	}
}

func TestNew_Level(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		wantErr   bool
		wantDebug bool
	}{
		{name: "empty defaults to info", level: ""},
		{name: "debug enables debug", level: "debug", wantDebug: true},
		{name: "invalid level is an error", level: "not-a-real-level", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, err := logger.New(logger.Options{Environment: "development", Level: tt.level}, &fakeLifecycle{})
			if tt.wantErr {
				if err == nil || !strings.HasPrefix(err.Error(), "logger: ") {
					t.Fatalf("New() error = %v, want a \"logger: ...\" error", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if got := log.Core().Enabled(zapcore.DebugLevel); got != tt.wantDebug {
				t.Errorf("debug enabled = %v, want %v", got, tt.wantDebug)
			}

			if !log.Core().Enabled(zapcore.InfoLevel) {
				t.Error("info should be enabled")
			}
		})
	}
}

func TestGetTraceID_RoundTripsWithTraceID(t *testing.T) {
	if got := logger.GetTraceID(context.Background()); got != "" {
		t.Fatalf("GetTraceID() on a plain context = %q, want empty", got)
	}

	ctx := logger.WithTraceID(context.Background(), "trace-123")
	if got := logger.GetTraceID(ctx); got != "trace-123" {
		t.Fatalf("GetTraceID() = %q, want %q", got, "trace-123")
	}
}

func TestGetTraceID_NilContext(t *testing.T) {
	if got := logger.GetTraceID(nil); got != "" {
		t.Fatalf("GetTraceID(nil) = %q, want empty", got)
	}
}

func TestGetTraceID_IgnoresRawStringKey(t *testing.T) {
	// WithTraceID is the single source; a raw "trace_id" string key must not be read.
	//nolint:staticcheck
	ctx := context.WithValue(context.Background(), "trace_id", "raw-key-trace-id")
	if got := logger.GetTraceID(ctx); got != "" {
		t.Fatalf("GetTraceID() = %q, want empty", got)
	}
}

func TestProvideNamed_EachChannelResolvesByItsTag(t *testing.T) {
	dir := t.TempDir()

	defs := []logger.Definition{
		{Name: "app", Level: "info", Filename: filepath.Join(dir, "app.log")},
		{Name: "http", Level: "info", Filename: filepath.Join(dir, "http.log")},
		{Name: "sql", Level: "warn", Filename: filepath.Join(dir, "sql.log")},
	}
	shared := logger.Options{Environment: "production", MaxSize: 1}

	var appLogger, httpLogger, sqlLogger, plainLogger *zap.Logger

	app := fxtest.New(t,
		fx.Options(logger.ProvideNamed(defs, shared)...),
		fx.Populate(
			fx.Annotate(&appLogger, fx.ParamTags(`name:"app"`)),
			fx.Annotate(&httpLogger, fx.ParamTags(`name:"http"`)),
			fx.Annotate(&sqlLogger, fx.ParamTags(`name:"sql"`)),
			&plainLogger,
		),
	)
	app.RequireStart()
	defer app.RequireStop()

	if appLogger == nil || httpLogger == nil || sqlLogger == nil {
		t.Fatal("expected all three named loggers to resolve, got a nil one")
	}

	if plainLogger == nil {
		t.Fatal("expected the \"app\" channel to also resolve as a plain, untagged *zap.Logger")
	}

	if appLogger != plainLogger {
		t.Error("the untagged *zap.Logger should be the same instance as the \"app\"-tagged one")
	}

	appLogger.Info("app channel message")
	httpLogger.Warn("http channel message")
	sqlLogger.Warn("sql channel message")
	for _, l := range []*zap.Logger{appLogger, httpLogger, sqlLogger} {
		// Best-effort flush of the buffered file output; the file assertions below catch failures.
		_ = l.Sync()
	}

	assertFileContains(t, filepath.Join(dir, "app.log"), "app channel message")
	assertFileContains(t, filepath.Join(dir, "http.log"), "http channel message")
	assertFileContains(t, filepath.Join(dir, "sql.log"), "sql channel message")
}

func TestProvideNamed_UnnamedDefinitionDefaultsToApp(t *testing.T) {
	dir := t.TempDir()

	defs := []logger.Definition{
		{Level: "info", Filename: filepath.Join(dir, "app.log")},
	}

	var appLogger *zap.Logger
	app := fxtest.New(t,
		fx.Options(logger.ProvideNamed(defs, logger.Options{Environment: "production"})...),
		fx.Populate(fx.Annotate(&appLogger, fx.ParamTags(`name:"app"`))),
	)
	app.RequireStart()
	defer app.RequireStop()

	if appLogger == nil {
		t.Fatal("a Definition with an empty Name should still resolve under name:\"app\"")
	}
}

// assertFileContains fails the test if path does not exist or does not contain want.
func assertFileContains(t *testing.T, path, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	if !strings.Contains(string(data), want) {
		t.Fatalf("%s = %q, want it to contain %q", path, data, want)
	}
}

func TestNew_FileOutputIsBufferedUntilFlushed(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "app.log")
	lc := &fakeLifecycle{}

	log, err := logger.New(logger.Options{Environment: "production", Level: "info", Filename: logPath, MaxSize: 1}, lc)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	log.Info("buffered line")

	if data, _ := os.ReadFile(logPath); strings.Contains(string(data), "buffered line") {
		t.Fatal("line reached the file before a flush, want it buffered")
	}

	if err := lc.hooks[0].OnStop(context.Background()); err != nil {
		t.Fatalf("OnStop() error = %v", err)
	}

	if data, _ := os.ReadFile(logPath); !strings.Contains(string(data), "buffered line") {
		t.Fatal("line missing after OnStop, want it flushed")
	}
}
