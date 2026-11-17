package middleware

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSOptions configures CORS middleware.
type CORSOptions struct {
	// AllowOrigins lists permitted origins; use "*" to allow any origin (not with AllowCredentials).
	AllowOrigins []string

	// AllowMethods lists permitted HTTP methods (e.g. GET, POST).
	AllowMethods []string

	// AllowHeaders lists permitted request headers.
	AllowHeaders []string

	// AllowCredentials sets the Access-Control-Allow-Credentials header.
	AllowCredentials bool

	// MaxAge is the preflight cache duration in seconds (Access-Control-Max-Age).
	MaxAge int
}

// CORS creates a middleware that validates cross-origin requests. It panics when AllowOrigins holds
// "*" with AllowCredentials, which would let any site make credentialed requests.
//
// Usage:
//
//	router.Use(middleware.CORS(middleware.CORSOptions{
//	    AllowOrigins: []string{"https://app.example.com"},
//	    AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
//	    AllowHeaders: []string{"Authorization", "Content-Type"},
//	}))
func CORS(opts CORSOptions) gin.HandlerFunc {
	allowHeaders := strings.Join(opts.AllowHeaders, ", ")
	allowMethods := strings.Join(opts.AllowMethods, ", ")
	maxAge := strconv.Itoa(opts.MaxAge)
	allowAll := slices.Contains(opts.AllowOrigins, "*")
	if allowAll && opts.AllowCredentials {
		panic("middleware: CORS AllowOrigins \"*\" cannot be combined with AllowCredentials; list the origins")
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		c.Writer.Header().Add("Vary", "Origin")

		if origin != "" {
			isAllowed := allowAll || slices.Contains(opts.AllowOrigins, origin)
			if isAllowed {
				if allowAll {
					c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				}

				if opts.AllowCredentials {
					c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
				}
			}
		}

		c.Writer.Header().Set("Access-Control-Allow-Headers", allowHeaders)
		c.Writer.Header().Set("Access-Control-Allow-Methods", allowMethods)
		c.Writer.Header().Set("Access-Control-Max-Age", maxAge)

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
