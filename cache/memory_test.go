package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jun-develop/junkit/cache"
)

func TestMemory_GetSet(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})

	if _, ok := c.Get(ctx, "missing"); ok {
		t.Fatal("Get() on an empty cache reported a hit")
	}

	if err := c.Set(ctx, "key", "value", 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, ok := c.Get(ctx, "key")
	if !ok {
		t.Fatal("Get() after Set() reported a miss")
	}

	if got != "value" {
		t.Fatalf("Get() = %q, want %q", got, "value")
	}
}

func TestMemory_Delete(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[int](cache.MemoryOptions{})

	_ = c.Set(ctx, "key", 42, 0)
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, ok := c.Get(ctx, "key"); ok {
		t.Fatal("Get() after Delete() reported a hit")
	}

	// Deleting an already-absent key is not an error.
	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() on a missing key error = %v, want nil", err)
	}
}

func TestMemory_Exists(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})

	if exists, err := c.Exists(ctx, "key"); err != nil || exists {
		t.Fatalf("Exists() = %v, %v, want false, nil", exists, err)
	}

	_ = c.Set(ctx, "key", "value", 0)

	if exists, err := c.Exists(ctx, "key"); err != nil || !exists {
		t.Fatalf("Exists() = %v, %v, want true, nil", exists, err)
	}
}

func TestMemory_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})

	if err := c.Set(ctx, "key", "value", 200*time.Millisecond); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if _, ok := c.Get(ctx, "key"); !ok {
		t.Fatal("Get() immediately after Set() with a TTL reported a miss")
	}

	time.Sleep(300 * time.Millisecond)

	if _, ok := c.Get(ctx, "key"); ok {
		t.Fatal("Get() after the TTL elapsed reported a hit")
	}
}

func TestMemory_GetOrSet(t *testing.T) {
	ctx := context.Background()

	t.Run("a hit never calls fetchFn", func(t *testing.T) {
		c := cache.NewMemory[string](cache.MemoryOptions{})
		_ = c.Set(ctx, "key", "cached", 0)

		called := false
		got, err := c.GetOrSet(ctx, "key", 0, func() (string, error) {
			called = true
			return "fetched", nil
		})
		if err != nil {
			t.Fatalf("GetOrSet() error = %v", err)
		}

		if got != "cached" {
			t.Fatalf("GetOrSet() = %q, want the cached value %q", got, "cached")
		}

		if called {
			t.Fatal("GetOrSet() called fetchFn on a cache hit")
		}
	})

	t.Run("a miss calls fetchFn and caches its result", func(t *testing.T) {
		c := cache.NewMemory[string](cache.MemoryOptions{})

		got, err := c.GetOrSet(ctx, "key", 0, func() (string, error) {
			return "fetched", nil
		})
		if err != nil {
			t.Fatalf("GetOrSet() error = %v", err)
		}

		if got != "fetched" {
			t.Fatalf("GetOrSet() = %q, want %q", got, "fetched")
		}

		cached, ok := c.Get(ctx, "key")
		if !ok || cached != "fetched" {
			t.Fatalf("value was not cached after GetOrSet: Get() = %q, %v", cached, ok)
		}
	})

	t.Run("fetchFn's error is returned and nothing is cached", func(t *testing.T) {
		c := cache.NewMemory[string](cache.MemoryOptions{})
		fetchErr := errors.New("upstream unavailable")

		_, err := c.GetOrSet(ctx, "key", 0, func() (string, error) {
			return "", fetchErr
		})
		if !errors.Is(err, fetchErr) {
			t.Fatalf("GetOrSet() error = %v, want %v", err, fetchErr)
		}

		if _, ok := c.Get(ctx, "key"); ok {
			t.Fatal("a failed fetchFn should not populate the cache")
		}
	})
}

func TestMemory_GetOfExpiredEntryKeepsConcurrentSet(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})

	for i := 0; i < 2000; i++ {
		_ = c.Set(ctx, "key", "stale", time.Nanosecond)
		time.Sleep(time.Microsecond)

		done := make(chan struct{})
		go func() {
			c.Get(ctx, "key")
			close(done)
		}()
		_ = c.Set(ctx, "key", "fresh", 0)
		<-done

		if got, ok := c.Get(ctx, "key"); !ok || got != "fresh" {
			t.Fatalf("iteration %d: Get() = %q, %v; a concurrent Set was lost", i, got, ok)
		}
	}
}

func TestMemory_Lookup(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})
	_ = c.Set(ctx, "key", "value", 0)

	tests := []struct {
		name    string
		key     string
		want    string
		wantHit bool
	}{
		{name: "hit", key: "key", want: "value", wantHit: true},
		{name: "miss", key: "missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hit, err := c.Lookup(ctx, tt.key)
			if err != nil || hit != tt.wantHit || got != tt.want {
				t.Fatalf("Lookup() = %q, %v, %v; want %q, %v, nil", got, hit, err, tt.want, tt.wantHit)
			}
		})
	}
}

func TestMemory_MaxEntries(t *testing.T) {
	ctx := context.Background()

	t.Run("a new key in a full cache evicts one entry", func(t *testing.T) {
		c := cache.NewMemory[int](cache.MemoryOptions{MaxEntries: 3})
		for i, k := range []string{"a", "b", "c", "d"} {
			_ = c.Set(ctx, k, i, 0)
		}

		if _, ok := c.Get(ctx, "d"); !ok {
			t.Fatal("the newest key was not stored")
		}

		present := 0
		for _, k := range []string{"a", "b", "c", "d"} {
			if _, ok := c.Get(ctx, k); ok {
				present++
			}
		}

		if present != 3 {
			t.Fatalf("%d entries present, want MaxEntries = 3", present)
		}
	})

	t.Run("overwriting an existing key evicts nothing", func(t *testing.T) {
		c := cache.NewMemory[int](cache.MemoryOptions{MaxEntries: 2})
		_ = c.Set(ctx, "a", 1, 0)
		_ = c.Set(ctx, "b", 2, 0)
		_ = c.Set(ctx, "a", 3, 0)

		if a, ok := c.Get(ctx, "a"); !ok || a != 3 {
			t.Fatalf("Get(a) = %d, %v; want 3, true", a, ok)
		}

		if _, ok := c.Get(ctx, "b"); !ok {
			t.Fatal("b was evicted by an overwrite")
		}
	})

	t.Run("Incr of a new key in a full cache evicts one entry", func(t *testing.T) {
		c := cache.NewMemory[int64](cache.MemoryOptions{MaxEntries: 1})
		_, _ = c.Incr(ctx, "a", 0)
		if n, err := c.Incr(ctx, "b", 0); err != nil || n != 1 {
			t.Fatalf("Incr(b) = %d, %v; want 1, nil", n, err)
		}

		if _, ok := c.Get(ctx, "a"); ok {
			t.Fatal("a is still present in a cache capped at one entry")
		}
	})
}
