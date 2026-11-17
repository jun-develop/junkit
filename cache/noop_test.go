package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jun-develop/junkit/cache"
)

func TestNoop(t *testing.T) {
	ctx := context.Background()
	c := cache.NewNoop[string]()

	if err := c.Set(ctx, "key", "value", time.Minute); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	if _, ok := c.Get(ctx, "key"); ok {
		t.Fatal("Get() reported a hit right after Set() — Noop must never retain anything")
	}

	if exists, err := c.Exists(ctx, "key"); err != nil || exists {
		t.Fatalf("Exists() = %v, %v, want false, nil", exists, err)
	}

	if err := c.Delete(ctx, "key"); err != nil {
		t.Fatalf("Delete() error = %v, want nil", err)
	}

	if _, hit, err := c.Lookup(ctx, "key"); hit || err != nil {
		t.Fatalf("Lookup() = %v, %v, want false, nil", hit, err)
	}
}

func TestNoop_GetOrSet(t *testing.T) {
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
			t.Fatal("GetOrSet()'s result should not be retained")
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
