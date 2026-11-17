package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/security"
	"github.com/jun-develop/junkit/tracer"
)

// bodyWriter discards response output when debug mode replaces it with the tracer dashboard.
type bodyWriter struct {
	gin.ResponseWriter
}

// Write implements io.Writer; it discards b.
func (w *bodyWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

// WriteHeader discards the status so the dashboard is still sent with 200 (e.g. after CORS aborts
// an OPTIONS request with 204).
func (w *bodyWriter) WriteHeader(int) {}

// WriteHeaderNow discards the commit for the same reason as WriteHeader.
func (w *bodyWriter) WriteHeaderNow() {}

// TracerDebugOptions selects debug requests: ?QueryKey=QueryValue plus the SecretHeader carrying
// Secret. Any empty field disables debug mode.
type TracerDebugOptions struct {
	QueryKey     string
	QueryValue   string
	SecretHeader string
	Secret       string
}

// Matches reports whether c is a debug request.
func (o TracerDebugOptions) Matches(c *gin.Context) bool {
	return o.QueryKey != "" && o.QueryValue != "" && o.SecretHeader != "" && o.Secret != "" &&
		c.Query(o.QueryKey) == o.QueryValue && security.SecureEqual(c.GetHeader(o.SecretHeader), o.Secret)
}

// TracerDebug renders an interactive debug dashboard for requests matching opts.
//
// Usage:
//
//	router.Use(middleware.TracerDebug(middleware.TracerDebugOptions{
//	    QueryKey: "t_debug", QueryValue: "0604", SecretHeader: "X-Internal-Secret", Secret: secret,
//	}))
func TracerDebug(opts TracerDebugOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !opts.Matches(c) {
			c.Next()
			return
		}

		start := time.Now()
		ctx := tracer.WithContext(c.Request.Context(), tracer.New())
		c.Request = c.Request.WithContext(ctx)

		bw := &bodyWriter{ResponseWriter: c.Writer}
		c.Writer = bw

		defer renderTracerDashboard(c, bw, start)

		c.Next()
	}
}

// renderTracerDashboard renders the debug dashboard as HTML, or as JSON for CLI clients.
func renderTracerDashboard(c *gin.Context, bw *bodyWriter, start time.Time) {
	if r := recover(); r != nil {
		if _, ok := r.(tracer.BreakpointSignal); !ok {
			panic(r)
		}

		tracer.Comment(c.Request.Context(), fmt.Sprintf("⏸ Stopped by tracer.Stop() after %v", time.Since(start)))
	} else {
		tracer.Comment(c.Request.Context(), fmt.Sprintf("Request processed in %v", time.Since(start)))
		tracer.HTTPInfo(c, c.Request)
	}

	c.Writer = bw.ResponseWriter

	// Set by inner middleware (Gzip) or the handler for the discarded body; the dashboard is plain.
	c.Writer.Header().Del("Content-Encoding")
	c.Writer.Header().Del("Content-Length")
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")

	// Set Content-Type explicitly: c.Data won't replace one the handler already set.
	if isTerminalClient(c) {
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.Data(http.StatusOK, "application/json; charset=utf-8", tracer.RenderJSON(c.Request.Context()))
	} else {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(tracer.RenderHTML(c.Request.Context())))
	}
}

// isTerminalClient reports whether the request's User-Agent identifies a CLI HTTP client.
func isTerminalClient(c *gin.Context) bool {
	ua := strings.ToLower(c.GetHeader("User-Agent"))
	return strings.Contains(ua, "curl") || strings.Contains(ua, "httpie") || strings.Contains(ua, "wget")
}
