package postgres_test

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/jun-develop/junkit/database/postgres"
)

func TestNewPoolConfig_AppliesOptions(t *testing.T) {
	const dsn = "postgres://user:secret@localhost:5432/app?pool_max_conns=7"

	tests := []struct {
		name         string
		opts         postgres.Options
		log          *zap.Logger
		wantMaxConns int32
		wantLifetime time.Duration
		wantTracer   bool
		wantErr      bool
	}{
		{
			name:         "zero options keep the DSN and pgx defaults",
			opts:         postgres.Options{DSN: dsn},
			wantMaxConns: 7,
			wantLifetime: time.Hour,
		},
		{
			name:         "set options override the DSN",
			opts:         postgres.Options{DSN: dsn, MaxConnection: 20, MaxConnLifetime: time.Minute},
			wantMaxConns: 20,
			wantLifetime: time.Minute,
		},
		{
			name:         "query logging attaches a tracer",
			opts:         postgres.Options{DSN: dsn, QueryLogging: true},
			log:          zap.NewNop(),
			wantMaxConns: 7,
			wantLifetime: time.Hour,
			wantTracer:   true,
		},
		{
			name:         "query logging without a logger attaches no tracer",
			opts:         postgres.Options{DSN: dsn, QueryLogging: true},
			wantMaxConns: 7,
			wantLifetime: time.Hour,
		},
		{
			name:    "an invalid DSN is an error",
			opts:    postgres.Options{DSN: "postgres://user:secret@localhost:notaport/app"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := postgres.NewPoolConfigForTest(tt.opts, tt.log)
			if tt.wantErr {
				if err == nil {
					t.Fatal("error = nil, want one")
				}

				return
			}

			if err != nil {
				t.Fatalf("error = %v, want nil", err)
			}

			if cfg.MaxConns != tt.wantMaxConns || cfg.MaxConnLifetime != tt.wantLifetime {
				t.Errorf("MaxConns, MaxConnLifetime = %d, %v, want %d, %v",
					cfg.MaxConns, cfg.MaxConnLifetime, tt.wantMaxConns, tt.wantLifetime)
			}

			if got := cfg.ConnConfig.Tracer != nil; got != tt.wantTracer {
				t.Errorf("tracer attached = %v, want %v", got, tt.wantTracer)
			}
		})
	}
}

func TestNewPoolConfig_ErrorHidesPassword(t *testing.T) {
	_, err := postgres.NewPoolConfigForTest(postgres.Options{DSN: "postgres://user:s3cr3t@localhost:bad/app"}, nil)
	if err == nil {
		t.Fatal("error = nil, want one")
	}

	if got := err.Error(); strings.Contains(got, "s3cr3t") {
		t.Errorf("error %q leaks the password", got)
	}
}
