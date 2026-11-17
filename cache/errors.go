package cache

import "errors"

var (
	// ErrNotInteger is returned by Incr on a cache that cannot hold int64 values.
	ErrNotInteger = errors.New("cache: Incr requires Cache[int64] or Cache[any]")
)
