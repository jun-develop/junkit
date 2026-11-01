// Package slicex holds slice helpers the standard slices package lacks.
package slicex

// Map returns fn applied to each element of s, in order. It returns an empty, non-nil slice for an
// empty s, so a JSON list renders as [] rather than null.
//
// Usage:
//
//	return slicex.Map(rows, mapUser), nil
func Map[S ~[]E, E, R any](s S, fn func(E) R) []R {
	out := make([]R, len(s))
	for i, v := range s {
		out[i] = fn(v)
	}

	return out
}
