package cache_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: memory_test.go
// Purpose: Verify the in-memory cache: get/set, delete, exists, TTL, GetOrSet, Lookup and size cap.
// Type: Unit
//
// Test functions: 8
// Total scenarios: 13
//
// #   Test Case                                       Scenario                                   Expected Result
// 1   TestMemory_GetReturnsValueStoredBySet           Get before and after Set                   Miss before, stored value after
// 2   TestMemory_DeleteRemovesKey                     Delete a stored key, then delete again     Key is gone; second Delete returns no error
// 3   TestMemory_ExistsReportsStoredKeys              Exists before and after Set                false before, true after
// 4   TestMemory_EntryExpiresAfterTTL                 Set with a 200ms TTL, wait 300ms           Hit right after Set, miss after the TTL
// 5   TestMemory_GetOrSetFetchesOnlyOnMiss            Key already cached                         Returns cached value; fetchFn not called
// 6   TestMemory_GetOrSetFetchesOnlyOnMiss            Key missing                                Returns and caches fetchFn's value
// 7   TestMemory_GetOrSetFetchesOnlyOnMiss            fetchFn fails                              Returns its error; nothing cached
// 8   TestMemory_GetOfExpiredEntryKeepsConcurrentSet  Get of an expired key races a Set (2000x)  The concurrent Set is never lost
// 9   TestMemory_LookupReportsHitOrMiss               Stored key                                 Returns the value and hit
// 10  TestMemory_LookupReportsHitOrMiss               Missing key                                Returns zero value and miss
// 11  TestMemory_MaxEntriesBoundsSize                 New key in a full cache                    Newest key stored; size stays at MaxEntries
// 12  TestMemory_MaxEntriesBoundsSize                 Overwrite an existing key in a full cache  Nothing evicted; new value stored
// 13  TestMemory_MaxEntriesBoundsSize                 Incr of a new key in a full cache          Old entry evicted; Incr returns 1
//
// =============================================================================

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jun-develop/junkit/cache"
)

// mustSet stores value under key and fails the test on error.
func mustSet[V any](t *testing.T, c cache.Cache[V], key string, value V, ttl time.Duration) {
	t.Helper()
	if err := c.Set(context.Background(), key, value, ttl); err != nil {
		t.Fatalf("Set(%q) error = %v", key, err)
	}
}

func TestMemory_GetReturnsValueStoredBySet(t *testing.T) {
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

func TestMemory_DeleteRemovesKey(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[int](cache.MemoryOptions{})
	mustSet(t, c, "key", 42, 0)

	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, ok := c.Get(ctx, "key"); ok {
		t.Fatal("Get() after Delete() reported a hit")
	}

	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() on a missing key error = %v, want nil", err)
	}
}

func TestMemory_ExistsReportsStoredKeys(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})

	exists, err := c.Exists(ctx, "key")
	if err != nil {
		t.Fatalf("Exists() before Set error = %v", err)
	}

	if exists {
		t.Fatal("Exists() before Set = true, want false")
	}

	mustSet(t, c, "key", "value", 0)

	exists, err = c.Exists(ctx, "key")
	if err != nil {
		t.Fatalf("Exists() after Set error = %v", err)
	}

	if !exists {
		t.Fatal("Exists() after Set = false, want true")
	}
}

func TestMemory_EntryExpiresAfterTTL(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})
	mustSet(t, c, "key", "value", 200*time.Millisecond)

	if _, ok := c.Get(ctx, "key"); !ok {
		t.Fatal("Get() immediately after Set() with a TTL reported a miss")
	}

	time.Sleep(300 * time.Millisecond)

	if _, ok := c.Get(ctx, "key"); ok {
		t.Fatal("Get() after the TTL elapsed reported a hit")
	}
}

func TestMemory_GetOrSetFetchesOnlyOnMiss(t *testing.T) {
	ctx := context.Background()

	t.Run("a hit never calls fetchFn", func(t *testing.T) {
		c := cache.NewMemory[string](cache.MemoryOptions{})
		mustSet(t, c, "key", "cached", 0)

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
		if !ok {
			t.Fatal("Get() after GetOrSet() reported a miss, want the fetched value cached")
		}

		if cached != "fetched" {
			t.Fatalf("Get() after GetOrSet() = %q, want %q", cached, "fetched")
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
			t.Fatal("Get() after a failed fetchFn reported a hit, want nothing cached")
		}
	})
}

// TestMemory_GetOfExpiredEntryKeepsConcurrentSet guards a race: a Get that removes an expired
// entry must not delete a fresh value a concurrent Set has just stored.
func TestMemory_GetOfExpiredEntryKeepsConcurrentSet(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})

	for i := 0; i < 2000; i++ {
		mustSet(t, c, "key", "stale", time.Nanosecond)
		time.Sleep(time.Microsecond)

		done := make(chan struct{})
		go func() {
			c.Get(ctx, "key")
			close(done)
		}()
		mustSet(t, c, "key", "fresh", 0)
		<-done

		got, ok := c.Get(ctx, "key")
		if !ok {
			t.Fatalf("iteration %d: Get() reported a miss; a concurrent Set was lost", i)
		}

		if got != "fresh" {
			t.Fatalf("iteration %d: Get() = %q, want %q", i, got, "fresh")
		}
	}
}

func TestMemory_LookupReportsHitOrMiss(t *testing.T) {
	ctx := context.Background()
	c := cache.NewMemory[string](cache.MemoryOptions{})
	mustSet(t, c, "key", "value", 0)

	tests := []struct {
		name    string
		key     string
		want    string
		wantHit bool
	}{
		{name: "stored key", key: "key", want: "value", wantHit: true},
		{name: "missing key", key: "missing", want: "", wantHit: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hit, err := c.Lookup(ctx, tt.key)

			if err != nil {
				t.Fatalf("Lookup(%q) error = %v, want nil", tt.key, err)
			}

			if hit != tt.wantHit {
				t.Errorf("Lookup(%q) hit = %v, want %v", tt.key, hit, tt.wantHit)
			}

			if got != tt.want {
				t.Errorf("Lookup(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestMemory_MaxEntriesBoundsSize(t *testing.T) {
	ctx := context.Background()

	t.Run("a new key in a full cache evicts one entry", func(t *testing.T) {
		c := cache.NewMemory[int](cache.MemoryOptions{MaxEntries: 3})
		keys := []string{"a", "b", "c", "d"}
		for i, k := range keys {
			mustSet(t, c, k, i, 0)
		}

		if _, ok := c.Get(ctx, "d"); !ok {
			t.Fatal("the newest key was not stored")
		}

		present := 0
		for _, k := range keys {
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
		mustSet(t, c, "a", 1, 0)
		mustSet(t, c, "b", 2, 0)
		mustSet(t, c, "a", 3, 0)

		a, ok := c.Get(ctx, "a")
		if !ok {
			t.Fatal("Get(a) reported a miss after overwrite")
		}

		if a != 3 {
			t.Fatalf("Get(a) = %d, want 3", a)
		}

		if _, ok := c.Get(ctx, "b"); !ok {
			t.Fatal("b was evicted by an overwrite")
		}
	})

	t.Run("Incr of a new key in a full cache evicts one entry", func(t *testing.T) {
		c := cache.NewMemory[int64](cache.MemoryOptions{MaxEntries: 1})
		if _, err := c.Incr(ctx, "a", 0); err != nil {
			t.Fatalf("Incr(a) error = %v", err)
		}

		n, err := c.Incr(ctx, "b", 0)
		if err != nil {
			t.Fatalf("Incr(b) error = %v", err)
		}

		if n != 1 {
			t.Fatalf("Incr(b) = %d, want 1", n)
		}

		if _, ok := c.Get(ctx, "a"); ok {
			t.Fatal("a is still present in a cache capped at one entry")
		}
	})
}
