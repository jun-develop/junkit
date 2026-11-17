// Package cache provides a generic key-value cache on Redis, in-memory or no-op backends.
//
// Build one Store per app (cache.Module does it and closes it on stop), then any number of
// typed caches on it; every Redis cache shares the Store's single connection pool.
//
// Usage:
//
//	s := cache.NewStore(cache.Options{Driver: cache.DriverRedis, Redis: redisOpts})
//	defer s.Close()
//	users := cache.New[*User](s)
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache is a typed key-value store implemented by Redis, Memory and Noop.
// Lookup reports whether key was found and any backend error; Get is Lookup with the error
// treated as a miss. Set stores a value for ttl (0 = no expiry); Incr atomically adds 1 and
// returns the new value, applying ttl only when it creates the key (a fixed window); GetOrSet
// calls fetchFn on a miss and stores the result.
type Cache[T any] interface {
	Lookup(ctx context.Context, key string) (T, bool, error)
	Get(ctx context.Context, key string) (T, bool)
	Set(ctx context.Context, key string, value T, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	GetOrSet(ctx context.Context, key string, ttl time.Duration, fetchFn func() (T, error)) (T, error)
}

// Driver selects which Cache implementation New constructs.
type Driver string

// Driver values accepted by NewStore.
const (
	// DriverRedis backs the cache with Redis.
	DriverRedis Driver = "redis"
	// DriverMemory backs the cache with in-memory storage.
	DriverMemory Driver = "memory"
	// DriverNoop disables caching.
	DriverNoop Driver = "noop"
)

// Options selects and configures a cache Store.
type Options struct {
	// Driver selects the backend (empty value defaults to DriverNoop).
	Driver Driver

	// Redis configures the connection when Driver is DriverRedis.
	Redis RedisOptions

	// Memory configures memory caches, used when Driver is DriverMemory or after OrMemory.
	Memory MemoryOptions
}

// withDefaults returns o with defaults applied to its Redis and Memory options.
func (o Options) withDefaults() Options {
	o.Redis = o.Redis.withDefaults()
	o.Memory = o.Memory.withDefaults()

	return o
}

// Store is the shared storage that caches are built on: one Redis client per app, or none for
// the memory and no-op drivers.
type Store struct {
	opts   Options
	client *redis.Client
}

// NewStore creates a Store for opts.Driver; with DriverRedis it opens a lazily connecting
// client that Close releases.
func NewStore(opts Options) *Store {
	opts = opts.withDefaults()
	s := &Store{opts: opts}
	if opts.Driver == DriverRedis {
		s.client = newRedisClient(opts.Redis)
	}

	return s
}

// OrMemory returns s unless it uses Redis, otherwise a memory Store. Use it for counters and
// blocks that must work without Redis (a no-op cache would never block), accepting that each
// instance then keeps its own.
//
// Usage:
//
//	counters := cache.New[int64](s.OrMemory())
func (s *Store) OrMemory() *Store {
	if s.opts.Driver == DriverRedis {
		return s
	}

	opts := s.opts
	opts.Driver = DriverMemory

	return &Store{opts: opts}
}

// Close releases the Redis client, if any. Caches built on s must not be used afterwards.
func (s *Store) Close() error {
	if s.client == nil {
		return nil
	}

	if err := s.client.Close(); err != nil {
		return fmt.Errorf("cache: close: %w", err)
	}

	return nil
}

// New builds a Cache[T] on s; Redis caches share s's client and memory caches each get their
// own map.
func New[T any](s *Store) Cache[T] {
	switch s.opts.Driver {
	case DriverRedis:
		return NewRedis[T](s.client)
	case DriverMemory:
		return NewMemory[T](s.opts.Memory)
	default:
		return NewNoop[T]()
	}
}
