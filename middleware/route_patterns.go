package middleware

import (
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jun-develop/junkit/response"
)

// slugPattern is the validation package's slug rule, so a route accepts the slugs it stores.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-.][a-z0-9]+)*$`)

// RoutePatterns rejects a request with 404 when a path parameter named in patterns does not match
// its rule, like Laravel's Route::pattern. Parameters without a rule are not checked.
//
// Usage:
//
//	router.Use(middleware.RoutePatterns(map[string]func(string) bool{
//	    "uuid": middleware.IsUUID,
//	    "id":   middleware.IsID,
//	    "slug": middleware.IsSlug,
//	}, responder))
func RoutePatterns(patterns map[string]func(string) bool, responder response.Responder) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, p := range c.Params {
			if match, ok := patterns[p.Key]; ok && !match(p.Value) {
				responder.Error(c, ErrAPINotFound)
				c.Abort()

				return
			}
		}

		c.Next()
	}
}

// IsUUID reports whether v is a UUID in its canonical 8-4-4-4-12 form.
func IsUUID(v string) bool {
	_, err := uuid.Parse(v)
	return err == nil && len(v) == 36
}

// IsID reports whether v is a positive integer that fits in int64.
func IsID(v string) bool {
	n, err := strconv.ParseInt(v, 10, 64)
	return err == nil && n > 0
}

// IsSlug reports whether v is lowercase letters and digits joined by single hyphens or dots.
func IsSlug(v string) bool {
	return slugPattern.MatchString(v)
}
