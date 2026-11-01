package cache

import "go.uber.org/fx"

// Driver selects which Cache implementation New constructs.
type Driver string

// Driver values accepted by New.
const (
	// DriverRedis backs the cache with Redis.
	DriverRedis Driver = "redis"
	// DriverMemory backs the cache with in-memory storage.
	DriverMemory Driver = "memory"
	// DriverNoop disables caching.
	DriverNoop Driver = "noop"
)

// Options selects and configures a Cache backend.
type Options struct {
	// Driver selects the backend (empty value defaults to DriverNoop).
	Driver Driver

	// Redis configures the connection when Driver is DriverRedis.
	Redis RedisOptions
}

// OrMemory returns opts with DriverMemory unless they select Redis. Use it for counters and
// blocks that must work without Redis (a no-op cache would never block), accepting that each
// instance then keeps its own.
//
// Usage:
//
//	counters := cache.New[int64](opts.OrMemory())
func (o Options) OrMemory() Options {
	if o.Driver != DriverRedis {
		o.Driver = DriverMemory
	}

	return o
}

// New builds a Cache[T] for the backend selected by opts.Driver.
//
// Usage:
//
//	userCache := cache.New[*domain.User](cache.Options{Driver: cache.DriverRedis, Redis: redisOpts})
func New[T any](opts Options) Cache[T] {
	switch opts.Driver {
	case DriverRedis:
		return NewRedisCache[T](opts.Redis)
	case DriverMemory:
		return NewMemoryCache[T]()
	default:
		return NewNoopCache[T]()
	}
}

// Module provides an Fx module that builds a Cache[any] instance from Options.
//
// Usage:
//
//	fx.New(
//	    fx.Supply(cache.Options{Driver: cache.DriverRedis, Redis: redisOpts}),
//	    cache.Module,
//	)
var Module = fx.Module("cache",
	fx.Provide(func(opts Options) Cache[any] {
		return New[any](opts)
	}),
)
