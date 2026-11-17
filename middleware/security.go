package middleware

import "github.com/gin-gonic/gin"

// SecurityOptions configures Security.
type SecurityOptions struct {
	// ForceHSTS sends Strict-Transport-Security on every response, not only on TLS connections.
	// Enable it when a TLS-terminating proxy sits in front, since the app then never sees TLS.
	ForceHSTS bool

	// CrossOriginResourcePolicy is the Cross-Origin-Resource-Policy value. Defaults to "same-site";
	// set "cross-origin" when other sites embed this server's resources (e.g. uploaded images).
	CrossOriginResourcePolicy string
}

// withDefaults fills unset fields.
func (o SecurityOptions) withDefaults() SecurityOptions {
	if o.CrossOriginResourcePolicy == "" {
		o.CrossOriginResourcePolicy = "same-site"
	}

	return o
}

// Security creates a Gin middleware that attaches standard HTTP security headers to responses.
//
// Usage:
//
//	router.Use(middleware.Security(middleware.SecurityOptions{ForceHSTS: true}))
func Security(opts SecurityOptions) gin.HandlerFunc {
	opts = opts.withDefaults()

	return func(c *gin.Context) {
		if opts.ForceHSTS || c.Request.TLS != nil {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}

		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		c.Header("Cross-Origin-Resource-Policy", opts.CrossOriginResourcePolicy)
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Writer.Header().Del("Server")

		c.Next()
	}
}
