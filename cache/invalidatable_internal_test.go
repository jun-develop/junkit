package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newTestInvalidatable returns an in-memory Invalidatable whose clock the test sets through *now.
func newTestInvalidatable(ttl time.Duration) (*Invalidatable[string], *time.Time) {
	c := NewInvalidatable[string](Options{Driver: DriverMemory}, ttl)
	now := time.Unix(1_000_000, 0)
	c.now = func() time.Time { return now }

	return c, &now
}

func TestInvalidatable(t *testing.T) {
	ctx := context.Background()
	markers := func(v string) []string { return []string{"group:" + v} }

	t.Run("caches until a marker of the value is invalidated", func(t *testing.T) {
		c, now := newTestInvalidatable(time.Minute)
		loads := 0
		load := func() (string, error) { loads++; return "a", nil }

		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, load); hit {
			t.Fatal("first GetOrLoad hit, want a miss")
		}

		if v, hit, _ := c.GetOrLoad(ctx, "k", markers, load); !hit || v != "a" || loads != 1 {
			t.Fatalf("second GetOrLoad = %q, hit %v, loads %d; want a cached \"a\"", v, hit, loads)
		}

		*now = now.Add(10 * time.Second)
		_ = c.Invalidate(ctx, "group:other")
		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, load); !hit {
			t.Fatal("an unrelated marker made the entry stale")
		}

		_ = c.Invalidate(ctx, "group:a")
		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, load); hit || loads != 2 {
			t.Fatalf("after Invalidate: hit %v, loads %d; want a reload", hit, loads)
		}
	})

	t.Run("a load that started before the invalidation stays stale", func(t *testing.T) {
		c, now := newTestInvalidatable(time.Minute)
		start := *now
		_, _, _ = c.GetOrLoad(ctx, "k", markers, func() (string, error) {
			// The data changes while this read is running: its result is already old.
			*now = start.Add(10 * time.Second)
			_ = c.Invalidate(ctx, "group:a")
			return "a", nil
		})

		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil }); hit {
			t.Fatal("entry loaded before the invalidation was served")
		}
	})

	t.Run("entries loaded within the clock-skew margin after a marker are stale", func(t *testing.T) {
		c, now := newTestInvalidatable(time.Minute)
		_ = c.Invalidate(ctx, "group:a")

		*now = now.Add(clockSkewMargin - time.Millisecond)
		_, _, _ = c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil })
		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil }); hit {
			t.Fatal("entry inside the skew margin was served")
		}

		*now = now.Add(time.Second)
		_, _, _ = c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil })
		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil }); !hit {
			t.Fatal("entry loaded well after the marker was not served")
		}
	})

	t.Run("Delete drops the entry", func(t *testing.T) {
		c, _ := newTestInvalidatable(time.Minute)
		_, _, _ = c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil })
		_ = c.Delete(ctx, "k")
		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil }); hit {
			t.Fatal("entry served after Delete")
		}
	})

	t.Run("a failed load is not cached", func(t *testing.T) {
		c, _ := newTestInvalidatable(time.Minute)
		errDB := errors.New("db down")
		if _, _, err := c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "", errDB }); !errors.Is(err, errDB) {
			t.Fatalf("err = %v, want %v", err, errDB)
		}

		if _, hit, _ := c.GetOrLoad(ctx, "k", markers, func() (string, error) { return "a", nil }); hit {
			t.Fatal("a failed load was cached")
		}
	})

	t.Run("ttl <= 0 always loads", func(t *testing.T) {
		c, _ := newTestInvalidatable(0)
		loads := 0
		for range 2 {
			_, _, _ = c.GetOrLoad(ctx, "k", markers, func() (string, error) { loads++; return "a", nil })
		}

		if loads != 2 {
			t.Fatalf("loads = %d, want 2", loads)
		}
	})
}

func TestOptions_OrMemory(t *testing.T) {
	if got := (Options{}).OrMemory().Driver; got != DriverMemory {
		t.Errorf("empty driver → %q, want memory", got)
	}

	if got := (Options{Driver: DriverNoop}).OrMemory().Driver; got != DriverMemory {
		t.Errorf("noop → %q, want memory", got)
	}

	if got := (Options{Driver: DriverRedis}).OrMemory().Driver; got != DriverRedis {
		t.Errorf("redis → %q, want redis", got)
	}
}
