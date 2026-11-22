// Package cache provides generic key-value caching backed by Redis, memory, or no-op.
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

// Cache is a generic key-value cache interface (Redis, Memory, or Noop).
type Cache[T any] interface {
	Lookup(ctx context.Context, key string) (T, bool, error)
	Get(ctx context.Context, key string) (T, bool)
	Set(ctx context.Context, key string, value T, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	GetOrSet(ctx context.Context, key string, ttl time.Duration, fetchFn func() (T, error)) (T, error)
}

// Driver selects the cache backend implementation.
type Driver string

// Supported cache drivers.
const (
	DriverRedis  Driver = "redis"
	DriverMemory Driver = "memory"
	DriverNoop   Driver = "noop"
)

// Options configures a cache Store.
type Options struct {
	// Driver selects the backend (defaults to DriverNoop).
	Driver Driver

	// Redis options used when Driver is DriverRedis.
	Redis RedisOptions

	// Memory options used when Driver is DriverMemory.
	Memory MemoryOptions
}

// withDefaults applies defaults to Redis and Memory options.
func (o Options) withDefaults() Options {
	o.Redis = o.Redis.withDefaults()
	o.Memory = o.Memory.withDefaults()

	return o
}

// Store manages shared cache storage and connections.
type Store struct {
	opts   Options
	client *redis.Client
}

// NewStore creates a Store configured with opts.
func NewStore(opts Options) *Store {
	opts = opts.withDefaults()
	s := &Store{opts: opts}
	if opts.Driver == DriverRedis {
		s.client = newRedisClient(opts.Redis)
	}

	return s
}

// OrMemory returns s if Redis is used, otherwise a memory-backed Store.
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

// Close releases underlying connections (e.g. Redis client).
func (s *Store) Close() error {
	if s.client == nil {
		return nil
	}

	if err := s.client.Close(); err != nil {
		return fmt.Errorf("cache: close: %w", err)
	}

	return nil
}

// New creates a typed Cache[T] backed by s.
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
