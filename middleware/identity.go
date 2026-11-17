package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/security"
)

// SetIdentity stores identity in c's request context, where security.GetIdentity finds it. Read it
// back from c.Request.Context(), or from c itself when the engine sets ContextWithFallback.
//
// Usage:
//
//	middleware.SetIdentity(c, identity)
func SetIdentity[T any](c *gin.Context, identity T) {
	c.Request = c.Request.WithContext(security.WithIdentity(c.Request.Context(), identity))
}
