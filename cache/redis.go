package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisOptions configures the shared Redis client's connection pool and timeouts.
type RedisOptions struct {
	// Addr is the Redis server address, e.g. "localhost:6379".
	Addr string

	// Password authenticates the connection; leave empty when auth is disabled.
	Password string

	// DB selects the Redis logical database (0-15).
	DB int

	// PoolSize is the maximum number of connections in the pool. Defaults to 10 * GOMAXPROCS.
	PoolSize int

	// MinIdleConns is the minimum number of idle connections kept open. Defaults to 5.
	MinIdleConns int

	// DialTimeout bounds connection establishment. Defaults to 5s.
	DialTimeout time.Duration

	// ReadTimeout bounds a single read. Defaults to 3s.
	ReadTimeout time.Duration

	// WriteTimeout bounds a single write. Defaults to 3s.
	WriteTimeout time.Duration
}

// withDefaults returns o with zero or negative pool sizes and timeouts set to their defaults.
func (o RedisOptions) withDefaults() RedisOptions {
	if o.PoolSize <= 0 {
		o.PoolSize = 10 * runtime.GOMAXPROCS(0)
	}

	if o.MinIdleConns <= 0 {
		o.MinIdleConns = 5
	}

	if o.DialTimeout <= 0 {
		o.DialTimeout = 5 * time.Second
	}

	if o.ReadTimeout <= 0 {
		o.ReadTimeout = 3 * time.Second
	}

	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 3 * time.Second
	}

	return o
}

// newRedisClient opens a lazily connecting client configured by opts.
func newRedisClient(opts RedisOptions) *redis.Client {
	opts = opts.withDefaults()

	return redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Password:     opts.Password,
		DB:           opts.DB,
		PoolSize:     opts.PoolSize,
		MinIdleConns: opts.MinIdleConns,
		DialTimeout:  opts.DialTimeout,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
	})
}

// incrScript increments KEYS[1] and sets its expiry (ARGV[1] ms) if it has none, atomically.
var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if tonumber(ARGV[1]) > 0 and redis.call('PTTL', KEYS[1]) < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`)

// Redis is a generic Cache backed by Redis; values are JSON-encoded. It does not own its client.
type Redis[T any] struct {
	client *redis.Client
}

// NewRedis creates a Redis cache on client; the caller keeps ownership of client and closes it.
func NewRedis[T any](client *redis.Client) *Redis[T] {
	return &Redis[T]{client: client}
}

// Ping checks whether the Redis server is reachable.
func (c *Redis[T]) Ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("cache: ping: %w", err)
	}

	return nil
}

// Lookup implements Cache; a missing key is a miss, a failed read or decode is an error.
func (c *Redis[T]) Lookup(ctx context.Context, key string) (T, bool, error) {
	var result T

	val, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return result, false, nil
	}

	if err != nil {
		return result, false, fmt.Errorf("cache: get: %w", err)
	}

	if err := json.Unmarshal(val, &result); err != nil {
		return result, false, fmt.Errorf("cache: decode: %w", err)
	}

	return result, true, nil
}

// Get implements Cache; use Lookup to tell a failed read from a miss.
func (c *Redis[T]) Get(ctx context.Context, key string) (T, bool) {
	val, ok, _ := c.Lookup(ctx, key)

	return val, ok
}

// Set implements Cache.
func (c *Redis[T]) Set(ctx context.Context, key string, value T, ttl time.Duration) error {
	bytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache: encode: %w", err)
	}

	if err := c.client.Set(ctx, key, bytes, ttl).Err(); err != nil {
		return fmt.Errorf("cache: set: %w", err)
	}

	return nil
}

// Incr implements Cache.
func (c *Redis[T]) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := incrScript.Run(ctx, c.client, []string{key}, ttl.Milliseconds()).Int64()
	if err != nil {
		return 0, fmt.Errorf("cache: incr: %w", err)
	}

	return n, nil
}

// Delete implements Cache.
func (c *Redis[T]) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("cache: delete: %w", err)
	}

	return nil
}

// Exists implements Cache.
func (c *Redis[T]) Exists(ctx context.Context, key string) (bool, error) {
	val, err := c.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("cache: exists: %w", err)
	}

	return val > 0, nil
}

// GetOrSet implements Cache; a failed read counts as a miss and a failed store is ignored,
// because fetchFn's value is still correct.
func (c *Redis[T]) GetOrSet(ctx context.Context, key string, ttl time.Duration, fetchFn func() (T, error)) (T, error) {
	if val, found := c.Get(ctx, key); found {
		return val, nil
	}

	val, err := fetchFn()
	if err != nil {
		var empty T

		return empty, err
	}

	// Best-effort: the caller has its value; the next call simply fetches again.
	_ = c.Set(ctx, key, val, ttl)

	return val, nil
}
