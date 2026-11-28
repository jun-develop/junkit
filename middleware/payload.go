package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/response"
)

// Gin context keys Payload populates.
const (
	// PayloadKey caches the body parsed as map[string]any (JSON or form) by the first GetPayload.
	PayloadKey = "_request_payload"

	// PayloadRawKey holds the raw request body bytes.
	PayloadRawKey = "_request_body_raw"
)

// defaultMaxBodySize is PayloadOptions.MaxBodySize's default.
const defaultMaxBodySize = 1 << 20

// PayloadOptions configures request body reading rules.
type PayloadOptions struct {
	// MaxBodySize is the maximum allowed request body size in bytes. Defaults to 1 MiB.
	MaxBodySize int64

	// RouteLimits overrides MaxBodySize per RouteKey (e.g. uploads); fill it before the server starts.
	RouteLimits map[string]int64
}

// withDefaults fills unset fields.
func (o PayloadOptions) withDefaults() PayloadOptions {
	if o.MaxBodySize <= 0 {
		o.MaxBodySize = defaultMaxBodySize
	}

	return o
}

// RouteKey returns the RouteLimits key for a route: its method and its Gin route pattern.
//
// Usage:
//
//	limits[middleware.RouteKey(http.MethodPost, "/api/v1/users/:uuid/avatar")] = 5 << 20
func RouteKey(method, pattern string) string {
	return method + " " + pattern
}

// Payload limits the request body and caches it in the Gin context for downstream handlers and
// the access log. A body over the limit gets 413, any other read failure 400. Multipart bodies are
// limited but not buffered, since uploads would otherwise sit in memory twice.
//
// Usage:
//
//	router.Use(middleware.Payload(middleware.PayloadOptions{MaxBodySize: 2 << 20}, responder))
func Payload(opts PayloadOptions, responder response.Responder) gin.HandlerFunc {
	opts = opts.withDefaults()

	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.ContentLength == 0 {
			c.Next()
			return
		}

		limit := opts.MaxBodySize
		if n, ok := opts.RouteLimits[RouteKey(c.Request.Method, c.FullPath())]; ok {
			limit = n
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		if strings.HasPrefix(c.ContentType(), "multipart/") {
			c.Next()
			return
		}

		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				responder.Error(c, ErrBodyTooLarge)
			} else {
				responder.Error(c, ErrInvalidBody)
			}

			c.Abort()

			return
		}

		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		c.Set(PayloadRawKey, bodyBytes)
		c.Next()
	}
}

// GetPayload returns the body cached by Payload parsed as a map (JSON objects and
// form-urlencoded bodies), or nil. It parses on the first call and caches the result.
func GetPayload(c *gin.Context) map[string]any {
	if val, ok := c.Get(PayloadKey); ok {
		payload, _ := val.(map[string]any)
		return payload
	}

	raw, ok := c.Get(PayloadRawKey)
	if !ok {
		return nil
	}

	bodyBytes, _ := raw.([]byte)
	payload := parsePayload(c.ContentType(), bodyBytes)
	c.Set(PayloadKey, payload)

	return payload
}

// parsePayload parses a JSON or form-urlencoded body into a map, or returns nil.
func parsePayload(contentType string, bodyBytes []byte) map[string]any {
	switch contentType {
	case "application/json":
		var payload map[string]any
		if json.Unmarshal(bodyBytes, &payload) != nil {
			return nil
		}

		return payload
	case "application/x-www-form-urlencoded":
		form, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			return nil
		}

		payload := make(map[string]any, len(form))
		for k, v := range form {
			if len(v) == 1 {
				payload[k] = v[0]
			} else {
				payload[k] = v
			}
		}

		return payload
	}

	return nil
}
