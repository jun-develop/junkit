package cache_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: noop_test.go
// Purpose: Verify that the Noop cache never stores anything and never fails.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 3
//
// #  Test Case                       Scenario                               Expected Result
// 1  TestNoop_RetainsNothing         Set, then Get, Exists, Delete, Lookup  No errors; every read is a miss
// 2  TestNoop_GetOrSetAlwaysFetches  fetchFn succeeds                       Returns fetchFn's value; nothing retained
// 3  TestNoop_GetOrSetAlwaysFetches  fetchFn fails                          Returns fetchFn's error
//
// =============================================================================

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jun-develop/junkit/cache"
)

func TestNoop_RetainsNothing(t *testing.T) {
	ctx := context.Background()
	c := cache.NewNoop[string]()

	if err := c.Set(ctx, "key", "value", time.Minute); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	if _, ok := c.Get(ctx, "key"); ok {
		t.Fatal("Get() reported a hit right after Set(), want a miss: Noop must never retain anything")
	}

	exists, err := c.Exists(ctx, "key")
	if err != nil {
		t.Fatalf("Exists() error = %v, want nil", err)
	}

	if exists {
		t.Fatal("Exists() = true, want false")
	}

	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	_, hit, err := c.Lookup(ctx, "key")
	if err != nil {
		t.Fatalf("Lookup() error = %v, want nil", err)
	}

	if hit {
		t.Fatal("Lookup() hit = true, want false")
	}
}

func TestNoop_GetOrSetAlwaysFetches(t *testing.T) {
	ctx := context.Background()
	c := cache.NewNoop[string]()

	t.Run("always calls fetchFn", func(t *testing.T) {
		called := false
		got, err := c.GetOrSet(ctx, "key", time.Minute, func() (string, error) {
			called = true
			return "fetched", nil
		})

		if err != nil {
			t.Fatalf("GetOrSet() error = %v", err)
		}

		if got != "fetched" {
			t.Fatalf("GetOrSet() = %q, want %q", got, "fetched")
		}

		if !called {
			t.Fatal("GetOrSet() did not call fetchFn")
		}

		if _, ok := c.Get(ctx, "key"); ok {
			t.Fatal("Get() after GetOrSet() reported a hit, want nothing retained")
		}
	})

	t.Run("propagates fetchFn's error", func(t *testing.T) {
		fetchErr := errors.New("upstream unavailable")

		_, err := c.GetOrSet(ctx, "key", time.Minute, func() (string, error) {
			return "", fetchErr
		})

		if !errors.Is(err, fetchErr) {
			t.Fatalf("GetOrSet() error = %v, want %v", err, fetchErr)
		}
	})
}
