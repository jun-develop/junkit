package cache_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: incr_test.go
// Purpose: Verify the Incr counter on the memory, noop and Redis caches.
// Type: Unit + Integration (TestRedis_IncrFollowsCounterContract needs REDIS_TEST_ADDR and
//       skips without it; when skipped, go test reports 1 skipped run instead of 4 scenarios)
//
// Test functions: 5
// Total scenarios: 12
//
// #   Test Case                              Scenario                                 Expected Result
// 1   TestMemory_IncrFollowsCounterContract  Three increments, then Get               Counts 1, 2, 3; Get returns 3
// 2   TestMemory_IncrFollowsCounterContract  Delete, then Incr                        Count restarts at 1
// 3   TestMemory_IncrFollowsCounterContract  Second Incr in the middle of the window  Expiry is not extended; key gone after the window
// 4   TestMemory_IncrFollowsCounterContract  20 workers x 50 increments in parallel   Final count is 1000 (no lost increments)
// 5   TestMemory_IncrWorksOnAnyCache         Two increments on a Cache[any]           Second Incr returns 2
// 6   TestMemory_IncrRejectsNonInteger       Incr on a Cache[string]                  Returns ErrNotInteger
// 7   TestMemory_IncrRejectsNonInteger       Incr on a string value in a Cache[any]   Returns ErrNotInteger
// 8   TestNoop_IncrAlwaysReturnsZero         Three increments                         Each returns 0 and no error
// 9   TestRedis_IncrFollowsCounterContract   Three increments, then Get (Redis)       Counts 1, 2, 3; Get returns 3
// 10  TestRedis_IncrFollowsCounterContract   Delete, then Incr (Redis)                Count restarts at 1
// 11  TestRedis_IncrFollowsCounterContract   Second Incr mid-window (Redis)           Expiry is not extended; key gone after the window
// 12  TestRedis_IncrFollowsCounterContract   Parallel increments (Redis)              Final count is 1000 (no lost increments)
//
// =============================================================================

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jun-develop/junkit/cache"
)

// incrContract runs the Incr scenarios every counting cache driver (memory, Redis) must pass.
func incrContract(t *testing.T, c cache.Cache[int64], key string) {
	t.Helper()
	ctx := context.Background()
	_ = c.Delete(ctx, key)
	t.Cleanup(func() { _ = c.Delete(ctx, key) })

	t.Run("counts from 1 and Get reads the value", func(t *testing.T) {
		for want := int64(1); want <= 3; want++ {
			n, err := c.Incr(ctx, key, time.Minute)
			if err != nil {
				t.Fatalf("Incr() error = %v", err)
			}

			if n != want {
				t.Fatalf("Incr() = %d, want %d", n, want)
			}
		}

		n, ok := c.Get(ctx, key)
		if !ok {
			t.Fatal("Get() hit = false, want true")
		}

		if n != 3 {
			t.Errorf("Get() = %d, want 3", n)
		}
	})

	t.Run("Delete restarts the count", func(t *testing.T) {
		if err := c.Delete(ctx, key); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		n, err := c.Incr(ctx, key, time.Minute)
		if err != nil {
			t.Fatalf("Incr() error = %v", err)
		}

		if n != 1 {
			t.Errorf("Incr() after Delete = %d, want 1", n)
		}
	})

	t.Run("fixed window: later increments do not extend the expiry", func(t *testing.T) {
		// The second Incr lands mid-window and the check at 1.25x: a sliding expiry would last
		// until 1.5x, so each side has a quarter-window margin.
		const window = 800 * time.Millisecond
		k := key + ":window"
		_ = c.Delete(ctx, k)
		t.Cleanup(func() { _ = c.Delete(ctx, k) })

		start := time.Now()
		if _, err := c.Incr(ctx, k, window); err != nil {
			t.Fatalf("first Incr() error = %v", err)
		}

		time.Sleep(window/2 - time.Since(start))
		if _, err := c.Incr(ctx, k, window); err != nil {
			t.Fatalf("second Incr() error = %v", err)
		}

		if time.Since(start) >= window {
			t.Skip("machine too slow: the second Incr missed the first window")
		}

		time.Sleep(window*5/4 - time.Since(start))

		if _, ok := c.Get(ctx, k); ok {
			t.Error("key still present after its first window ended")
		}

		n, err := c.Incr(ctx, k, window)
		if err != nil {
			t.Fatalf("Incr() after expiry error = %v", err)
		}

		if n != 1 {
			t.Errorf("Incr() after expiry = %d, want 1", n)
		}
	})

	t.Run("concurrent increments are not lost", func(t *testing.T) {
		k := key + ":concurrent"
		_ = c.Delete(ctx, k)
		t.Cleanup(func() { _ = c.Delete(ctx, k) })
		const workers, perWorker = 20, 50

		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				for range perWorker {
					if _, err := c.Incr(ctx, k, time.Minute); err != nil {
						t.Errorf("Incr() error = %v", err)
						return
					}
				}
			})
		}
		wg.Wait()

		if n, _ := c.Get(ctx, k); n != workers*perWorker {
			t.Errorf("count = %d, want %d", n, workers*perWorker)
		}
	})
}

func TestMemory_IncrFollowsCounterContract(t *testing.T) {
	incrContract(t, cache.NewMemory[int64](cache.MemoryOptions{}), "test:incr")
}

func TestMemory_IncrWorksOnAnyCache(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[any](cache.MemoryOptions{})
	if _, err := c.Incr(ctx, "k", time.Minute); err != nil {
		t.Fatalf("first Incr() error = %v", err)
	}

	n, err := c.Incr(ctx, "k", time.Minute)

	if err != nil {
		t.Fatalf("second Incr() error = %v", err)
	}

	if n != 2 {
		t.Errorf("second Incr() = %d, want 2", n)
	}
}

func TestMemory_IncrRejectsNonInteger(t *testing.T) {
	ctx := context.Background()

	t.Run("incr on a Cache[string]", func(t *testing.T) {
		c := cache.NewMemory[string](cache.MemoryOptions{})

		if _, err := c.Incr(ctx, "k", time.Minute); !errors.Is(err, cache.ErrNotInteger) {
			t.Errorf("Incr() error = %v, want ErrNotInteger", err)
		}
	})

	t.Run("incr on a string value in a Cache[any]", func(t *testing.T) {
		c := cache.NewMemory[any](cache.MemoryOptions{})
		if err := c.Set(ctx, "k", "text", time.Minute); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		if _, err := c.Incr(ctx, "k", time.Minute); !errors.Is(err, cache.ErrNotInteger) {
			t.Errorf("Incr() error = %v, want ErrNotInteger", err)
		}
	})
}

func TestNoop_IncrAlwaysReturnsZero(t *testing.T) {
	c := cache.NewNoop[int64]()

	for range 3 {
		n, err := c.Incr(context.Background(), "k", time.Minute)

		if err != nil {
			t.Fatalf("Incr() error = %v, want nil", err)
		}

		if n != 0 {
			t.Fatalf("Incr() = %d, want 0", n)
		}
	}
}

// TestRedis_IncrFollowsCounterContract is an integration test: it needs a Redis server at
// REDIS_TEST_ADDR (e.g. localhost:6379) and skips when it is unset or unreachable.
func TestRedis_IncrFollowsCounterContract(t *testing.T) {
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })
	c := cache.NewRedis[int64](rdb)
	if err := c.Ping(context.Background()); err != nil {
		t.Skipf("redis not reachable: %v", err)
	}

	incrContract(t, c, "junkit:test:incr")
}
