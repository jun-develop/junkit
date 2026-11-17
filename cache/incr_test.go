package cache_test

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

// incrContract checks Incr behavior shared by the Redis and memory drivers.
func incrContract(t *testing.T, c cache.Cache[int64], key string) {
	t.Helper()
	ctx := context.Background()
	_ = c.Delete(ctx, key)
	t.Cleanup(func() { _ = c.Delete(ctx, key) })

	t.Run("counts from 1 and Get reads the value", func(t *testing.T) {
		for want := int64(1); want <= 3; want++ {
			if n, err := c.Incr(ctx, key, time.Minute); err != nil || n != want {
				t.Fatalf("Incr = %d, %v; want %d", n, err, want)
			}
		}

		if n, ok := c.Get(ctx, key); !ok || n != 3 {
			t.Errorf("Get = %d, %v; want 3, true", n, ok)
		}
	})

	t.Run("Delete restarts the count", func(t *testing.T) {
		_ = c.Delete(ctx, key)
		if n, _ := c.Incr(ctx, key, time.Minute); n != 1 {
			t.Errorf("Incr after Delete = %d, want 1", n)
		}
	})

	t.Run("fixed window: later increments do not extend the expiry", func(t *testing.T) {
		// The second Incr lands mid-window and the check at 1.25x: a sliding expiry would last
		// until 1.5x, so each side has a quarter-window margin.
		const window = 800 * time.Millisecond
		k := key + ":window"
		_ = c.Delete(ctx, k)
		start := time.Now()
		_, _ = c.Incr(ctx, k, window)
		time.Sleep(window/2 - time.Since(start))
		_, _ = c.Incr(ctx, k, window)
		if time.Since(start) >= window {
			t.Skip("machine too slow: the second Incr missed the first window")
		}

		time.Sleep(window*5/4 - time.Since(start))
		if _, ok := c.Get(ctx, k); ok {
			t.Error("key still present after its first window ended")
		}

		if n, _ := c.Incr(ctx, k, window); n != 1 {
			t.Errorf("Incr after expiry = %d, want 1", n)
		}

		_ = c.Delete(ctx, k)
	})

	t.Run("concurrent increments are not lost", func(t *testing.T) {
		k := key + ":concurrent"
		_ = c.Delete(ctx, k)
		const workers, perWorker = 20, 50
		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				for range perWorker {
					if _, err := c.Incr(ctx, k, time.Minute); err != nil {
						t.Error(err)
						return
					}
				}
			})
		}

		wg.Wait()
		if n, _ := c.Get(ctx, k); n != workers*perWorker {
			t.Errorf("count = %d, want %d", n, workers*perWorker)
		}

		_ = c.Delete(ctx, k)
	})
}

func TestMemory_Incr(t *testing.T) {
	incrContract(t, cache.NewMemory[int64](cache.MemoryOptions{}), "test:incr")
}

func TestMemory_IncrOnAnyCache(t *testing.T) {
	c := cache.NewMemory[any](cache.MemoryOptions{})
	_, _ = c.Incr(context.Background(), "k", time.Minute)
	if n, err := c.Incr(context.Background(), "k", time.Minute); err != nil || n != 2 {
		t.Errorf("Incr = %d, %v; want 2", n, err)
	}
}

func TestMemory_IncrRejectsNonIntegerCache(t *testing.T) {
	c := cache.NewMemory[string](cache.MemoryOptions{})
	if _, err := c.Incr(context.Background(), "k", time.Minute); !errors.Is(err, cache.ErrNotInteger) {
		t.Errorf("Incr on Cache[string] err = %v, want ErrNotInteger", err)
	}

	anyCache := cache.NewMemory[any](cache.MemoryOptions{})
	_ = anyCache.Set(context.Background(), "k", "text", time.Minute)
	if _, err := anyCache.Incr(context.Background(), "k", time.Minute); !errors.Is(err, cache.ErrNotInteger) {
		t.Errorf("Incr on a string value err = %v, want ErrNotInteger", err)
	}
}

func TestNoop_Incr(t *testing.T) {
	c := cache.NewNoop[int64]()
	for range 3 {
		if n, err := c.Incr(context.Background(), "k", time.Minute); n != 0 || err != nil {
			t.Fatalf("Incr = %d, %v; want 0, nil", n, err)
		}
	}
}

// TestRedis_Incr runs only when REDIS_TEST_ADDR is set (e.g. localhost:6379).
func TestRedis_Incr(t *testing.T) {
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
