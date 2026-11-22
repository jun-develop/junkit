package cache

import (
	"context"
	"time"
)

// clockSkewMargin accounts for clock differences across instances when checking staleness.
const clockSkewMargin = 2 * time.Second

// stamped holds a cached value alongside its load timestamp.
type stamped[T any] struct {
	Value    T         `json:"value"`
	LoadedAt time.Time `json:"loaded_at"`
}

// Invalidatable provides tag-based cache invalidation using timestamps (e.g. for sessions/permissions).
type Invalidatable[T any] struct {
	entries Cache[*stamped[T]]
	markers Cache[int64]
	ttl     time.Duration
	now     func() time.Time
}

// NewInvalidatable creates an Invalidatable cache backed by s (ttl <= 0 disables caching).
//
// Usage:
//
//	sessions := cache.NewInvalidatable[*Session](s, 3*time.Minute)
func NewInvalidatable[T any](s *Store, ttl time.Duration) *Invalidatable[T] {
	return &Invalidatable[T]{
		entries: New[*stamped[T]](s),
		markers: New[int64](s),
		ttl:     ttl,
		now:     time.Now,
	}
}

// GetOrLoad returns the cached value if valid, or loads and caches it via load.
//
// Usage:
//
//	s, hit, err := sessions.GetOrLoad(
//	    ctx,
//	    "session:"+hash,
//	    func(s *Session) []string { return []string{"family:" + s.Family} },
//	    func() (*Session, error) { return repo.GetSession(ctx, hash) },
//	)
func (c *Invalidatable[T]) GetOrLoad(ctx context.Context, key string, markers func(T) []string, load func() (T, error)) (value T, hit bool, err error) {
	if c.ttl <= 0 {
		value, err = load()
		return value, false, err
	}

	if entry, ok := c.entries.Get(ctx, key); ok && entry != nil && !c.stale(ctx, entry, markers(entry.Value)) {
		return entry.Value, true, nil
	}

	// Timestamp before loading so concurrent invalidations mark this stale.
	loadedAt := c.now()
	if value, err = load(); err != nil {
		return value, false, err
	}

	_ = c.entries.Set(ctx, key, &stamped[T]{Value: value, LoadedAt: loadedAt}, c.ttl)

	return value, false, nil
}

// Delete removes key from the cache.
func (c *Invalidatable[T]) Delete(ctx context.Context, key string) error {
	if c.ttl <= 0 {
		return nil
	}

	return c.entries.Delete(ctx, key)
}

// Invalidate marks entries associated with marker as stale.
func (c *Invalidatable[T]) Invalidate(ctx context.Context, marker string) error {
	if c.ttl <= 0 {
		return nil
	}

	return c.markers.Set(ctx, marker, c.now().UnixNano(), 2*c.ttl)
}

// stale reports whether any of the entry's markers were invalidated after it was loaded.
func (c *Invalidatable[T]) stale(ctx context.Context, entry *stamped[T], markers []string) bool {
	loadedAt := entry.LoadedAt.UnixNano()
	for _, m := range markers {
		at, ok, err := c.markers.Lookup(ctx, m)
		if err != nil || (ok && at+clockSkewMargin.Nanoseconds() >= loadedAt) {
			return true
		}
	}

	return false
}
