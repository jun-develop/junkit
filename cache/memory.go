package cache

import (
	"context"
	"sync"
	"time"
)

// defaultMaxEntries bounds a Memory cache unless MemoryOptions.MaxEntries says otherwise.
const defaultMaxEntries = 100_000

// MemoryOptions configures a Memory cache.
type MemoryOptions struct {
	// MaxEntries caps the number of entries; storing a new key in a full cache evicts another.
	// Defaults to 100,000.
	MaxEntries int
}

// withDefaults returns o with a non-positive MaxEntries set to defaultMaxEntries.
func (o MemoryOptions) withDefaults() MemoryOptions {
	if o.MaxEntries <= 0 {
		o.MaxEntries = defaultMaxEntries
	}

	return o
}

// entry is one Memory slot: a value plus its absolute expiry.
type entry[T any] struct {
	value    T
	expireAt time.Time
}

// expired reports whether the entry is past its expiry as of now.
func (e entry[T]) expired(now time.Time) bool {
	return !e.expireAt.IsZero() && now.After(e.expireAt)
}

// sweepInterval is the minimum time between sweeps of expired entries.
const sweepInterval = time.Minute

// Memory is an in-memory Cache backed by a Go map holding at most MaxEntries entries.
// Expired entries are swept on writes, at most once per sweepInterval.
type Memory[T any] struct {
	mu         sync.RWMutex
	items      map[string]entry[T]
	lastSweep  time.Time
	maxEntries int
}

// NewMemory creates an empty Memory cache.
func NewMemory[T any](opts MemoryOptions) *Memory[T] {
	opts = opts.withDefaults()

	return &Memory[T]{items: make(map[string]entry[T]), maxEntries: opts.MaxEntries}
}

// Lookup implements Cache; an expired entry is treated as a miss and evicted. It never fails.
func (c *Memory[T]) Lookup(_ context.Context, key string) (T, bool, error) {
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()

	var empty T
	if !ok {
		return empty, false, nil
	}

	if now := time.Now(); item.expired(now) {
		c.mu.Lock()
		if cur, ok := c.items[key]; ok && cur.expired(now) {
			delete(c.items, key)
		}

		c.mu.Unlock()

		return empty, false, nil
	}

	return item.value, true, nil
}

// Get implements Cache.
func (c *Memory[T]) Get(ctx context.Context, key string) (T, bool) {
	val, ok, _ := c.Lookup(ctx, key)

	return val, ok
}

// Set implements Cache.
func (c *Memory[T]) Set(_ context.Context, key string, value T, ttl time.Duration) error {
	now := time.Now()
	var expireAt time.Time
	if ttl > 0 {
		expireAt = now.Add(ttl)
	}

	c.mu.Lock()
	c.sweep(now)
	c.makeRoom(key)
	c.items[key] = entry[T]{value: value, expireAt: expireAt}
	c.mu.Unlock()

	return nil
}

// Incr implements Cache.
func (c *Memory[T]) Incr(_ context.Context, key string, ttl time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.sweep(now)
	var count int64
	item, ok := c.items[key]
	if ok && !item.expired(now) {
		current, isInt := any(item.value).(int64)
		if !isInt {
			return 0, ErrNotInteger
		}

		count = current
	} else {
		item = entry[T]{}
		if ttl > 0 {
			item.expireAt = now.Add(ttl)
		}
	}

	value, isT := any(count + 1).(T)
	if !isT {
		return 0, ErrNotInteger
	}

	item.value = value
	c.makeRoom(key)
	c.items[key] = item

	return count + 1, nil
}

// Delete implements Cache.
func (c *Memory[T]) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()

	return nil
}

// Exists implements Cache.
func (c *Memory[T]) Exists(ctx context.Context, key string) (bool, error) {
	_, found := c.Get(ctx, key)

	return found, nil
}

// GetOrSet implements Cache.
func (c *Memory[T]) GetOrSet(ctx context.Context, key string, ttl time.Duration, fetchFn func() (T, error)) (T, error) {
	if val, found := c.Get(ctx, key); found {
		return val, nil
	}

	val, err := fetchFn()
	if err != nil {
		var empty T

		return empty, err
	}

	// Memory's Set never fails.
	_ = c.Set(ctx, key, val, ttl)

	return val, nil
}

// sweep deletes expired entries once per sweepInterval; the caller must hold c.mu.
func (c *Memory[T]) sweep(now time.Time) {
	if now.Sub(c.lastSweep) < sweepInterval {
		return
	}

	c.lastSweep = now
	for key, item := range c.items {
		if item.expired(now) {
			delete(c.items, key)
		}
	}
}

// makeRoom evicts one entry when storing a new key would exceed maxEntries; the caller must
// hold c.mu.
func (c *Memory[T]) makeRoom(key string) {
	if _, ok := c.items[key]; ok || len(c.items) < c.maxEntries {
		return
	}

	// shortcut: evicts an arbitrary entry (map order), not the least recently used; switch to an
	// LRU list if hit rates under memory pressure matter.
	for k := range c.items {
		delete(c.items, k)

		return
	}
}
