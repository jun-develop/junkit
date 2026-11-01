package cache

import (
	"context"
	"time"
)

// clockSkewMargin also treats entries loaded this long after an invalidation as stale: with Redis
// the entry's time and the marker's come from different instances, whose clocks can differ.
const clockSkewMargin = 2 * time.Second

// stamped is a cached value and the time its load started.
type stamped[T any] struct {
	Value    T         `json:"value"`
	LoadedAt time.Time `json:"loaded_at"`
}

// Invalidatable caches values that must be dropped before their TTL when the data behind them
// changes, such as sessions or permissions. Invalidate stores the time under a marker; an entry
// whose load started before one of its markers is a miss. Because the load's start time is kept,
// a load that read the old data and finished after the invalidation is still treated as stale.
type Invalidatable[T any] struct {
	entries Cache[*stamped[T]]
	markers Cache[int64]
	ttl     time.Duration
	now     func() time.Time
}

// NewInvalidatable creates an Invalidatable on opts' backend; ttl <= 0 disables caching.
//
// Usage:
//
//	sessions := cache.NewInvalidatable[*Session](opts, 3*time.Minute)
func NewInvalidatable[T any](opts Options, ttl time.Duration) *Invalidatable[T] {
	return &Invalidatable[T]{entries: New[*stamped[T]](opts), markers: New[int64](opts), ttl: ttl, now: time.Now}
}

// GetOrLoad returns key's cached value unless one of markers(value) was invalidated after it was
// loaded; otherwise it calls load and caches the result. hit reports whether the value came from
// the cache. A memory backend shares cached values, so copy one before changing it.
//
// Usage:
//
//	s, hit, err := sessions.GetOrLoad(ctx, "session:"+hash,
//	    func(s *Session) []string { return []string{"family:" + s.Family} },
//	    func() (*Session, error) { return repo.GetSession(ctx, hash) })
func (c *Invalidatable[T]) GetOrLoad(ctx context.Context, key string, markers func(T) []string, load func() (T, error)) (value T, hit bool, err error) {
	if c.ttl <= 0 {
		value, err = load()
		return value, false, err
	}

	if entry, ok := c.entries.Get(ctx, key); ok && entry != nil && !c.stale(ctx, entry, markers(entry.Value)) {
		return entry.Value, true, nil
	}

	// Taken before the read so a concurrent invalidation still marks it stale.
	loadedAt := c.now()
	if value, err = load(); err != nil {
		return value, false, err
	}

	_ = c.entries.Set(ctx, key, &stamped[T]{Value: value, LoadedAt: loadedAt}, c.ttl)

	return value, false, nil
}

// Delete drops key's entry, so the next GetOrLoad loads it. A load still running can write it back;
// call Invalidate too when that matters.
func (c *Invalidatable[T]) Delete(ctx context.Context, key string) error {
	if c.ttl <= 0 {
		return nil
	}

	return c.entries.Delete(ctx, key)
}

// Invalidate marks every entry listing marker stale. The marker is kept for 2x TTL, since a slow
// load can write its entry after it; if storing it fails, those entries live until their TTL.
func (c *Invalidatable[T]) Invalidate(ctx context.Context, marker string) error {
	if c.ttl <= 0 {
		return nil
	}

	return c.markers.Set(ctx, marker, c.now().UnixNano(), 2*c.ttl)
}

// stale reports whether entry was loaded before one of markers was invalidated.
func (c *Invalidatable[T]) stale(ctx context.Context, entry *stamped[T], markers []string) bool {
	loadedAt := entry.LoadedAt.UnixNano()
	for _, m := range markers {
		if at, ok := c.markers.Get(ctx, m); ok && at+clockSkewMargin.Nanoseconds() >= loadedAt {
			return true
		}
	}

	return false
}
