package cache_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: module_test.go
// Purpose: Verify backend selection by driver (New, Store.OrMemory) and the fx Module lifecycle.
// Type: Unit (the Redis client is created but never connects)
//
// Test functions: 3
// Total scenarios: 10
//
// #   Test Case                            Scenario                   Expected Result
// 1   TestNew_PicksBackendByDriver         DriverMemory               Returns a Memory cache
// 2   TestNew_PicksBackendByDriver         DriverNoop                 Returns a Noop cache
// 3   TestNew_PicksBackendByDriver         Zero Options               Returns a Noop cache
// 4   TestNew_PicksBackendByDriver         Unknown driver             Returns a Noop cache
// 5   TestNew_PicksBackendByDriver         DriverRedis                Returns a Redis cache without connecting
// 6   TestStore_OrMemoryFallsBackToMemory  Empty driver               Memory cache
// 7   TestStore_OrMemoryFallsBackToMemory  DriverNoop                 Memory cache
// 8   TestStore_OrMemoryFallsBackToMemory  DriverMemory               Memory cache
// 9   TestStore_OrMemoryFallsBackToMemory  DriverRedis                Redis cache (kept)
// 10  TestModule_ProvidesAndClosesStore    Start and stop the fx app  Provides a *Store; OnStop closes its client
//
// =============================================================================

import (
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/jun-develop/junkit/cache"
)

// kind names the concrete backend of c: "memory", "noop", "redis" or "unknown".
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

func TestNew_PicksBackendByDriver(t *testing.T) {
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
			store := cache.NewStore(tt.opts)
			t.Cleanup(func() { _ = store.Close() })

			if got := kind(cache.New[string](store)); got != tt.want {
				t.Fatalf("New() kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStore_OrMemoryFallsBackToMemory(t *testing.T) {
	tests := []struct {
		name   string
		driver cache.Driver
		want   string
	}{
		{name: "empty driver", driver: "", want: "memory"},
		{name: "noop driver", driver: cache.DriverNoop, want: "memory"},
		{name: "memory driver", driver: cache.DriverMemory, want: "memory"},
		{name: "redis driver is kept", driver: cache.DriverRedis, want: "redis"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := cache.NewStore(cache.Options{Driver: tt.driver})
			t.Cleanup(func() { _ = store.Close() })

			if got := kind(cache.New[string](store.OrMemory())); got != tt.want {
				t.Fatalf("New(OrMemory()) kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModule_ProvidesAndClosesStore(t *testing.T) {
	var store *cache.Store
	app := fxtest.New(t,
		fx.Supply(cache.Options{Driver: cache.DriverRedis, Redis: cache.RedisOptions{Addr: "localhost:6379"}}),
		cache.Module,
		fx.Populate(&store),
	)

	app.RequireStart()
	app.RequireStop()

	if store == nil {
		t.Fatal("Module did not provide a *Store")
	}

	if err := store.Close(); err == nil {
		t.Fatal("Close() after stop error = nil, want an error: OnStop should have closed the client")
	}
}
