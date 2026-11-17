package security

import (
	"context"
	"fmt"
)

// identityKey is the private context key for the authenticated identity.
type identityKey struct{}

// WithIdentity returns a copy of ctx carrying identity; middleware.SetIdentity uses it for a request.
//
// Usage:
//
//	ctx := security.WithIdentity(context.Background(), identity)
func WithIdentity[T any](ctx context.Context, identity T) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

// GetIdentity returns the identity of type T from ctx. A *gin.Context only finds it when the engine
// sets ContextWithFallback, since the identity lives in the request context; otherwise pass
// c.Request.Context().
//
// Usage:
//
//	identity, ok := security.GetIdentity[*domain.Identity](ctx)
func GetIdentity[T any](ctx context.Context) (T, bool) {
	identity, ok := ctx.Value(identityKey{}).(T)

	return identity, ok
}

// MustGetIdentity is GetIdentity for routes behind an auth middleware; it panics when missing.
//
// Usage:
//
//	identity := security.MustGetIdentity[*domain.Identity](c.Request.Context())
func MustGetIdentity[T any](ctx context.Context) T {
	identity, ok := GetIdentity[T](ctx)
	if !ok {
		var zero T
		panic(fmt.Sprintf("security: no identity of type %T in context", zero))
	}

	return identity
}
