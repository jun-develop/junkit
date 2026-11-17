package cache

import (
	"context"
	"time"
)

// Noop is a Cache that disables caching: every read misses and writes are ignored.
type Noop[T any] struct{}

// NewNoop creates a Noop cache.
func NewNoop[T any]() *Noop[T] {
	return &Noop[T]{}
}

// Lookup implements Cache; it always reports a miss.
func (c *Noop[T]) Lookup(context.Context, string) (T, bool, error) {
	var empty T

	return empty, false, nil
}

// Get implements Cache; it always reports a miss.
func (c *Noop[T]) Get(context.Context, string) (T, bool) {
	var empty T

	return empty, false
}

// Set implements Cache; it is a no-op that always succeeds.
func (c *Noop[T]) Set(context.Context, string, T, time.Duration) error {
	return nil
}

// Incr implements Cache; it always returns 0.
func (c *Noop[T]) Incr(context.Context, string, time.Duration) (int64, error) {
	return 0, nil
}

// Delete implements Cache; it is a no-op that always succeeds.
func (c *Noop[T]) Delete(context.Context, string) error {
	return nil
}

// Exists implements Cache; it always reports false.
func (c *Noop[T]) Exists(context.Context, string) (bool, error) {
	return false, nil
}

// GetOrSet implements Cache; it always calls fetchFn and discards the result.
func (c *Noop[T]) GetOrSet(_ context.Context, _ string, _ time.Duration, fetchFn func() (T, error)) (T, error) {
	return fetchFn()
}
