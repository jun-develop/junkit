// Package logger provides a zap.Logger with console/file output and trace ID propagation.
package logger

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// contextKey is an unexported type for context keys to prevent collisions.
type contextKey string

// TraceIDKey is the context key for storing the request trace ID.
const TraceIDKey contextKey = "trace_id"

// Output values for Options.Output.
const (
	OutputConsole = "console"
	OutputFile    = "file"
)

// Options configures the logger.
type Options struct {
	// Environment selects the output: "development" logs to the stdout console; any other value
	// logs JSON to the rotating Filename.
	Environment string

	// Output overrides Environment's choice of output: OutputConsole or OutputFile. Empty keeps
	// the Environment-based default.
	Output string

	// Level is the minimum log level (debug, info, warn, error); invalid values fall back to info.
	Level string

	// Filename is the log file path used outside development.
	Filename string

	// MaxSize is the max log file size in MB before rotating.
	MaxSize int

	// MaxBackups is the max number of rotated log files to retain.
	MaxBackups int

	// MaxAge is the max days to retain old log files.
	MaxAge int

	// Compress enables gzip compression for rotated files.
	Compress bool
}

// New builds a *zap.Logger from opts and flushes it on Fx stop. Development logs to the stdout
// console; other environments log JSON to a rotating file, buffered so a busy server does not make
// one write syscall per line (each behind the file's lock). The buffer is flushed every second, when
// it fills, on Fx stop, and on a Panic or Fatal entry; a crash can still lose the last second.
//
// Usage:
//
//	l, err := logger.New(lc, logger.Options{
//	    Environment: "production", Level: "info", Filename: "logs/app.log",
//	})
func New(lc fx.Lifecycle, opts Options) (*zap.Logger, error) {
	var level zapcore.Level
	if err := level.Set(opts.Level); err != nil {
		level = zapcore.InfoLevel
	}

	output := opts.Output
	if output == "" && opts.Environment == "development" {
		output = OutputConsole
	}

	var (
		core zapcore.Core
		stop func() error
	)
	
	if output == OutputConsole {
		consoleEncoderCfg := zap.NewDevelopmentEncoderConfig()
		consoleEncoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
		consoleEncoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		consoleEncoder := zapcore.NewConsoleEncoder(consoleEncoderCfg)
		stdoutSync := zapcore.AddSync(os.Stdout)

		core = zapcore.NewCore(consoleEncoder, stdoutSync, level)
	} else {
		fileEncoderCfg := zap.NewProductionEncoderConfig()
		fileEncoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
		fileEncoder := zapcore.NewJSONEncoder(fileEncoderCfg)

		fileWriter := &zapcore.BufferedWriteSyncer{
			WS: zapcore.AddSync(&lumberjack.Logger{
				Filename:   opts.Filename,
				MaxSize:    opts.MaxSize,
				MaxBackups: opts.MaxBackups,
				MaxAge:     opts.MaxAge,
				Compress:   opts.Compress,
			}),
			Size:          256 << 10,
			FlushInterval: time.Second,
		}
		stop = fileWriter.Stop

		core = zapcore.NewCore(fileEncoder, fileWriter, level)
	}

	zapLogger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			_ = zapLogger.Sync()
			if stop != nil {
				_ = stop()
			}

			return nil
		},
	})

	return zapLogger, nil
}

// Module provides the Fx module for building *zap.Logger from Options.
//
// Usage:
//
//	fx.New(fx.Supply(logger.Options{Environment: "development", Level: "debug"}), logger.Module)
var Module = fx.Module("logger",
	fx.Provide(New),
)

// Definition configures a named logging channel for ProvideNamed.
type Definition struct {
	// Name is the Fx name tag (`name:"<Name>"`). Defaults to "app".
	Name string

	// Level is the minimum log level for this channel.
	Level string

	// Filename is the log file path for this channel.
	Filename string
}

// ProvideNamed provides a name-tagged *zap.Logger per Definition, built with the shared Options.
// The "app" channel is also provided untagged as the default logger.
//
// Usage:
//
//	fx.New(fx.Options(logger.ProvideNamed([]logger.Definition{
//	    {Name: "app", Level: "info", Filename: "logs/app.log"},
//	    {Name: "http", Level: "info", Filename: "logs/http.log"},
//	}, logger.Options{Environment: "production", MaxSize: 10})...))
func ProvideNamed(defs []Definition, shared Options) []fx.Option {
	opts := make([]fx.Option, 0, len(defs)+1)

	for _, def := range defs {
		name := def.Name
		if name == "" {
			name = "app"
		}

		constructor := func(lc fx.Lifecycle) (*zap.Logger, error) {
			return New(lc, Options{
				Environment: shared.Environment,
				Output:      shared.Output,
				Level:       def.Level,
				Filename:    def.Filename,
				MaxSize:     shared.MaxSize,
				MaxBackups:  shared.MaxBackups,
				MaxAge:      shared.MaxAge,
				Compress:    shared.Compress,
			})
		}

		nameTag := fmt.Sprintf(`name:"%s"`, name)
		opts = append(opts, fx.Provide(fx.Annotate(constructor, fx.ResultTags(nameTag))))

		if name == "app" {
			opts = append(opts, fx.Provide(fx.Annotate(
				func(l *zap.Logger) *zap.Logger { return l },
				fx.ParamTags(nameTag),
			)))
		}
	}

	return opts
}

// WithTraceID returns a copy of ctx carrying traceID.
//
// Usage:
//
//	ctx = logger.WithTraceID(ctx, traceID)
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, TraceIDKey, traceID)
}

// GetTraceID returns the trace ID stored in ctx, or "" if there is none.
//
// Usage:
//
//	traceID := logger.GetTraceID(ctx)
func GetTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}

	if val := ctx.Value(TraceIDKey); val != nil {
		if traceID, ok := val.(string); ok {
			return traceID
		}
	}

	// Fallback for middlewares storing "trace_id" as a raw string key.
	if val := ctx.Value("trace_id"); val != nil {
		if traceID, ok := val.(string); ok {
			return traceID
		}
	}

	return ""
}
