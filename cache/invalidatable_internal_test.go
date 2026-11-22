package cache

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: invalidatable_internal_test.go
// Purpose: Verify that Invalidatable.GetOrLoad serves cached values only while no marker of the
//          value has been invalidated since the value was loaded, and that Delete and
//          Invalidate report store failures.
// Type: Unit (in-memory store, fake clock)
//
// Test functions: 2
// Total scenarios: 10
//
// #   Test Case                                               Scenario                                         Expected Result
// 1   TestInvalidatable_GetOrLoadServesOnlyFreshValues        Load, reload, invalidate other then own marker   Hit until its own marker is invalidated, then reloads
// 2   TestInvalidatable_GetOrLoadServesOnlyFreshValues        Marker invalidated while the load is running     Next call is a miss
// 3   TestInvalidatable_GetOrLoadServesOnlyFreshValues        Loaded within the clock-skew margin of a marker  Miss inside the margin, hit after it
// 4   TestInvalidatable_GetOrLoadServesOnlyFreshValues        Delete the key                                   Next call is a miss
// 5   TestInvalidatable_GetOrLoadServesOnlyFreshValues        Load returns an error                            Error returned; nothing cached
// 6   TestInvalidatable_GetOrLoadServesOnlyFreshValues        Marker store fails on read                       Next call is a miss
// 7   TestInvalidatable_GetOrLoadServesOnlyFreshValues        TTL is 0                                         Every call loads
// 8   TestInvalidatable_DeleteAndInvalidateReportStoreErrors  Entry store Delete fails                         Delete returns that error
// 9   TestInvalidatable_DeleteAndInvalidateReportStoreErrors  Marker store Set fails                           Invalidate returns that error
// 10  TestInvalidatable_DeleteAndInvalidateReportStoreErrors  TTL is 0, both stores failing                    Both return nil without touching the stores
//
// =============================================================================

import (
	"context"
	"errors"
	"testing"
	"time"
)

// failingMarkers is a Cache whose Lookup always fails, like an unreachable Redis.
type failingMarkers struct {
	Cache[int64]
}

// Lookup returns a backend error.
func (failingMarkers) Lookup(context.Context, string) (int64, bool, error) {
	return 0, false, errors.New("connection refused")
}

// failingWrites is a Cache whose Set and Delete always fail, like an unreachable Redis.
type failingWrites[T any] struct {
	Cache[T]
}

// Set returns a backend error.
func (failingWrites[T]) Set(context.Context, string, T, time.Duration) error {
	return errWriteFailed
}

// Delete returns a backend error.
func (failingWrites[T]) Delete(context.Context, string) error {
	return errWriteFailed
}

var errWriteFailed = errors.New("connection refused")

// newTestInvalidatable returns an in-memory Invalidatable whose clock the test sets through *now.
func newTestInvalidatable(ttl time.Duration) (*Invalidatable[string], *time.Time) {
	c := NewInvalidatable[string](NewStore(Options{Driver: DriverMemory}), ttl)
	now := time.Unix(1_000_000, 0)
	c.now = func() time.Time { return now }

	return c, &now
}

// testKey is the cache key every scenario reads and writes.
const testKey = "k"

// loadA is a load function that always succeeds with "a".
func loadA() (string, error) { return "a", nil }

// markersOf returns the single marker "group:<value>" of a cached value.
func markersOf(v string) []string { return []string{"group:" + v} }

// mustGetOrLoad calls GetOrLoad for testKey and fails the test on error.
func mustGetOrLoad(t *testing.T, c *Invalidatable[string], load func() (string, error)) (string, bool) {
	t.Helper()
	v, hit, err := c.GetOrLoad(context.Background(), testKey, markersOf, load)
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}

	return v, hit
}

// mustInvalidate invalidates marker and fails the test on error.
func mustInvalidate(t *testing.T, c *Invalidatable[string], marker string) {
	t.Helper()
	if err := c.Invalidate(context.Background(), marker); err != nil {
		t.Fatalf("Invalidate(%q) error = %v", marker, err)
	}
}

func TestInvalidatable_GetOrLoadServesOnlyFreshValues(t *testing.T) {
	ctx := context.Background()

	t.Run("caches until a marker of the value is invalidated", func(t *testing.T) {
		c, now := newTestInvalidatable(time.Minute)
		loads := 0
		load := func() (string, error) { loads++; return "a", nil }

		if _, hit := mustGetOrLoad(t, c, load); hit {
			t.Fatal("first GetOrLoad() hit = true, want a miss")
		}

		v, hit := mustGetOrLoad(t, c, load)
		if !hit {
			t.Fatal("second GetOrLoad() hit = false, want a hit")
		}

		if v != "a" {
			t.Fatalf("second GetOrLoad() = %q, want %q", v, "a")
		}

		if loads != 1 {
			t.Fatalf("loads after second GetOrLoad() = %d, want 1", loads)
		}

		*now = now.Add(10 * time.Second)
		mustInvalidate(t, c, "group:other")
		if _, hit := mustGetOrLoad(t, c, load); !hit {
			t.Fatal("GetOrLoad() after an unrelated marker hit = false, want a hit")
		}

		mustInvalidate(t, c, "group:a")
		if _, hit := mustGetOrLoad(t, c, load); hit {
			t.Fatal("GetOrLoad() after Invalidate hit = true, want a miss")
		}

		if loads != 2 {
			t.Fatalf("loads after Invalidate = %d, want 2", loads)
		}
	})

	t.Run("a load that started before the invalidation stays stale", func(t *testing.T) {
		c, now := newTestInvalidatable(time.Minute)
		start := *now
		mustGetOrLoad(t, c, func() (string, error) {
			// The data changes while this read is running: its result is already old.
			*now = start.Add(10 * time.Second)
			if err := c.Invalidate(ctx, "group:a"); err != nil {
				t.Errorf("Invalidate() error = %v", err)
			}

			return "a", nil
		})

		if _, hit := mustGetOrLoad(t, c, loadA); hit {
			t.Fatal("GetOrLoad() hit = true, want a miss: the value was loaded before the invalidation")
		}
	})

	t.Run("entries loaded within the clock-skew margin after a marker are stale", func(t *testing.T) {
		c, now := newTestInvalidatable(time.Minute)
		mustInvalidate(t, c, "group:a")

		*now = now.Add(clockSkewMargin - time.Millisecond)
		mustGetOrLoad(t, c, loadA)
		if _, hit := mustGetOrLoad(t, c, loadA); hit {
			t.Fatal("GetOrLoad() hit = true for a value loaded inside the skew margin, want a miss")
		}

		*now = now.Add(time.Second)
		mustGetOrLoad(t, c, loadA)
		if _, hit := mustGetOrLoad(t, c, loadA); !hit {
			t.Fatal("GetOrLoad() hit = false for a value loaded well after the marker, want a hit")
		}
	})

	t.Run("Delete drops the entry", func(t *testing.T) {
		c, _ := newTestInvalidatable(time.Minute)
		mustGetOrLoad(t, c, loadA)
		if err := c.Delete(ctx, testKey); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		if _, hit := mustGetOrLoad(t, c, loadA); hit {
			t.Fatal("GetOrLoad() after Delete hit = true, want a miss")
		}
	})

	t.Run("a failed load is not cached", func(t *testing.T) {
		c, _ := newTestInvalidatable(time.Minute)
		errDB := errors.New("db down")

		_, _, err := c.GetOrLoad(ctx, testKey, markersOf, func() (string, error) { return "", errDB })
		if !errors.Is(err, errDB) {
			t.Fatalf("GetOrLoad() error = %v, want %v", err, errDB)
		}

		if _, hit := mustGetOrLoad(t, c, loadA); hit {
			t.Fatal("GetOrLoad() after a failed load hit = true, want a miss")
		}
	})

	t.Run("a marker read error makes the entry stale", func(t *testing.T) {
		c, _ := newTestInvalidatable(time.Minute)
		mustGetOrLoad(t, c, loadA)
		c.markers = failingMarkers{Cache: c.markers}

		if _, hit := mustGetOrLoad(t, c, loadA); hit {
			t.Fatal("GetOrLoad() hit = true although the marker could not be read, want a miss")
		}
	})

	t.Run("ttl <= 0 always loads", func(t *testing.T) {
		c, _ := newTestInvalidatable(0)
		loads := 0
		for range 2 {
			mustGetOrLoad(t, c, func() (string, error) { loads++; return "a", nil })
		}

		if loads != 2 {
			t.Fatalf("loads = %d, want 2", loads)
		}
	})
}

func TestInvalidatable_DeleteAndInvalidateReportStoreErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("Delete returns the entry store error", func(t *testing.T) {
		c, _ := newTestInvalidatable(time.Minute)
		c.entries = failingWrites[*stamped[string]]{Cache: c.entries}

		if err := c.Delete(ctx, testKey); !errors.Is(err, errWriteFailed) {
			t.Errorf("Delete() error = %v, want %v", err, errWriteFailed)
		}
	})

	t.Run("Invalidate returns the marker store error", func(t *testing.T) {
		c, _ := newTestInvalidatable(time.Minute)
		c.markers = failingWrites[int64]{Cache: c.markers}

		if err := c.Invalidate(ctx, "group:a"); !errors.Is(err, errWriteFailed) {
			t.Errorf("Invalidate() error = %v, want %v", err, errWriteFailed)
		}
	})

	t.Run("ttl <= 0 skips the store entirely", func(t *testing.T) {
		c, _ := newTestInvalidatable(0)
		c.entries = failingWrites[*stamped[string]]{Cache: c.entries}
		c.markers = failingWrites[int64]{Cache: c.markers}

		if err := c.Delete(ctx, testKey); err != nil {
			t.Errorf("Delete() error = %v, want nil (caching disabled)", err)
		}

		if err := c.Invalidate(ctx, "group:a"); err != nil {
			t.Errorf("Invalidate() error = %v, want nil (caching disabled)", err)
		}
	})
}
