package cache_test

import (
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/jun-develop/junkit/cache"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name string
		opts cache.Options
		want string
	}{
		{name: "DriverMemory returns a Memory", opts: cache.Options{Driver: cache.DriverMemory}, want: "memory"},
		{name: "DriverNoop returns a Noop", opts: cache.Options{Driver: cache.DriverNoop}, want: "noop"},
		{name: "zero Options default to Noop", opts: cache.Options{}, want: "noop"},
		{name: "an unknown driver defaults to Noop", opts: cache.Options{Driver: "bogus"}, want: "noop"},
		{
			name: "DriverRedis returns a Redis without connecting",
			opts: cache.Options{Driver: cache.DriverRedis, Redis: cache.RedisOptions{Addr: "localhost:6379"}},
			want: "redis",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := cache.NewStore(tt.opts)
			t.Cleanup(func() { _ = b.Close() })
			if got := kind(cache.New[string](b)); got != tt.want {
				t.Fatalf("New() kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStore_OrMemory(t *testing.T) {
	tests := []struct {
		driver cache.Driver
		want   string
	}{
		{driver: "", want: "memory"},
		{driver: cache.DriverNoop, want: "memory"},
		{driver: cache.DriverMemory, want: "memory"},
		{driver: cache.DriverRedis, want: "redis"},
	}
	for _, tt := range tests {
		t.Run(string(tt.driver), func(t *testing.T) {
			b := cache.NewStore(cache.Options{Driver: tt.driver})
			t.Cleanup(func() { _ = b.Close() })
			if got := kind(cache.New[string](b.OrMemory())); got != tt.want {
				t.Fatalf("New(OrMemory()) kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModule_ProvidesAndClosesStore(t *testing.T) {
	var b *cache.Store
	app := fxtest.New(t,
		fx.Supply(cache.Options{Driver: cache.DriverRedis, Redis: cache.RedisOptions{Addr: "localhost:6379"}}),
		cache.Module,
		fx.Populate(&b),
	)
	app.RequireStart()
	app.RequireStop()

	if b == nil {
		t.Fatal("Module did not provide a *Store")
	}

	if err := b.Close(); err == nil {
		t.Fatal("Close after stop succeeded, want an error: OnStop should have closed the client")
	}
}

// kind names the concrete backend of c.
func kind(c cache.Cache[string]) string {
	switch c.(type) {
	case *cache.Memory[string]:
		return "memory"
	case *cache.Noop[string]:
		return "noop"
	case *cache.Redis[string]:
		return "redis"
	default:
		return "unknown"
	}
}
