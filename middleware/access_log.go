package middleware

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/response"
)

// defaultMaxAccessLogBodySize caps the response body size buffered for logging.
const defaultMaxAccessLogBodySize = 100 * 1024

// defaultExcludedPaths are paths skipped by AccessLog by default.
var defaultExcludedPaths = []string{"/health", "/metrics", "/favicon.ico"}

// defaultSensitiveFields are payload fields masked by default.
var defaultSensitiveFields = []string{
	"password", "pass", "token", "new_password",
	"access_token", "refresh_token", "secret", "authorization",
	"recaptcha_token", "api_key", "client_secret", "current_password", "old_password",
}

// sensitiveHints mask any field or query key whose normalized name contains one of them, so names
// such as password_confirmation or reset_token are masked without being listed.
var sensitiveHints = []string{"password", "token", "secret", "apikey", "authorization"}

// accessLogHostname is this host's name, attached to every access log entry.
var accessLogHostname, _ = os.Hostname()

// accessLogBufferPool reuses response body buffers to reduce allocations.
var accessLogBufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// AccessLogOptions configures AccessLog middleware.
type AccessLogOptions struct {
	// ExcludedPaths lists request paths to skip. Defaults to /health, /metrics and /favicon.ico.
	ExcludedPaths []string

	// SensitiveFields lists JSON field and query parameter names to mask at any depth
	// (case-insensitive, ignoring "_" and "-"). Defaults to common credential fields such as password,
	// token and authorization. Names containing password, token, secret, api key or authorization are
	// always masked.
	SensitiveFields []string

	// BodiesOnErrorOnly logs request and response payloads only for responses with status >= 400, so
	// the data of successful requests (often personal data) is not stored in the log.
	BodiesOnErrorOnly bool

	// MaxBodySize caps response bytes buffered for logging. Defaults to 100KB.
	MaxBodySize int

	// SkipIf dynamically skips logging for a request when returning true.
	SkipIf func(*gin.Context) bool
}

// accessLogResponseWriter wraps gin.ResponseWriter to buffer the response body.
type accessLogResponseWriter struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	maxSize int
}

// Write implements io.Writer; it also buffers up to maxSize bytes of the body for logging.
func (w *accessLogResponseWriter) Write(data []byte) (int, error) {
	if w.body.Len() < w.maxSize {
		remaining := w.maxSize - w.body.Len()
		if len(data) > remaining {
			w.body.Write(data[:remaining])
		} else {
			w.body.Write(data)
		}
	}

	return w.ResponseWriter.Write(data)
}

// AccessLog returns a middleware that logs structured HTTP request/response details.
//
// Usage:
//
//	router.Use(middleware.AccessLog(httpLogger, middleware.AccessLogOptions{}))
func AccessLog(httpLogger *zap.Logger, opts AccessLogOptions) gin.HandlerFunc {
	excluded := opts.ExcludedPaths
	if len(excluded) == 0 {
		excluded = defaultExcludedPaths
	}

	excludedSet := make(map[string]struct{}, len(excluded))
	for _, p := range excluded {
		excludedSet[p] = struct{}{}
	}

	sensitive := opts.SensitiveFields
	if len(sensitive) == 0 {
		sensitive = defaultSensitiveFields
	}

	sensitiveSet := make(map[string]struct{}, len(sensitive))
	for _, f := range sensitive {
		sensitiveSet[normalizeFieldName(f)] = struct{}{}
	}

	maxBodySize := opts.MaxBodySize
	if maxBodySize <= 0 {
		maxBodySize = defaultMaxAccessLogBodySize
	}

	return func(c *gin.Context) {
		if _, ok := excludedSet[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		if opts.SkipIf != nil && opts.SkipIf(c) {
			c.Next()
			return
		}

		start := time.Now()

		respBuf := accessLogBufferPool.Get().(*bytes.Buffer)
		respBuf.Reset()
		defer accessLogBufferPool.Put(respBuf)

		c.Writer = &accessLogResponseWriter{ResponseWriter: c.Writer, body: respBuf, maxSize: maxBodySize}

		c.Next()

		duration := time.Since(start)

		var reqBody, respBody any
		if !opts.BodiesOnErrorOnly || c.Writer.Status() >= 400 {
			reqBody = GetPayload(c)
			sanitizeInPlace(reqBody, sensitiveSet)

			respBody = parseAccessLogResponseBody(c.Writer.Header().Get("Content-Type"), respBuf.Bytes())
			sanitizeInPlace(respBody, sensitiveSet)
		}

		var errs any
		if len(c.Errors) > 0 {
			errs = c.Errors.ByType(gin.ErrorTypePrivate).String()
		}

		traceID, _ := c.Get(response.TraceIDContextKey)

		logFn := accessLogFuncForStatus(httpLogger, c.Writer.Status())
		logFn("HTTP_ACCESS_LOG",
			zap.Any("trace_id", traceID),
			zap.String("hostname", accessLogHostname),
			zap.Int64("latency_ms", duration.Milliseconds()),
			zap.Int("status", c.Writer.Status()),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("query", maskQuery(c.Request.URL.RawQuery, sensitiveSet)),
			zap.String("ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Int("resp_size", c.Writer.Size()),
			zap.Any("error", errs),
			zap.Any("request_payload", reqBody),
			zap.Any("response_payload", respBody),
			zap.Time("request_time", start),
		)
	}
}

// accessLogFuncForStatus picks Error for 5xx, Warn for 4xx and Info otherwise.
func accessLogFuncForStatus(logger *zap.Logger, status int) func(string, ...zap.Field) {
	switch {
	case status >= 500:
		return logger.Error
	case status >= 400:
		return logger.Warn
	default:
		return logger.Info
	}
}

// parseAccessLogResponseBody decodes a JSON body, or returns it as a string truncated to 200 bytes.
func parseAccessLogResponseBody(contentType string, data []byte) any {
	if len(data) == 0 {
		return nil
	}

	if strings.Contains(contentType, "application/json") {
		var result any
		if err := json.Unmarshal(data, &result); err == nil {
			return result
		}
	}

	if len(data) > 200 {
		return string(data[:200]) + "... [truncated]"
	}

	return string(data)
}

// sanitizeInPlace masks sensitive fields at any depth of nested JSON objects and arrays.
func sanitizeInPlace(data any, sensitive map[string]struct{}) {
	switch val := data.(type) {
	case map[string]any:
		for k, v := range val {
			if isSensitive(k, sensitive) {
				val[k] = "[MASKED]"
				continue
			}

			sanitizeInPlace(v, sensitive)
		}
	case []any:
		for _, item := range val {
			sanitizeInPlace(item, sensitive)
		}
	}
}

// maskQuery returns raw with the values of sensitive parameters masked; raw is kept as is when none
// is sensitive.
func maskQuery(raw string, sensitive map[string]struct{}) string {
	if raw == "" {
		return ""
	}

	values, _ := url.ParseQuery(raw)
	masked := false
	for k, vv := range values {
		if isSensitive(k, sensitive) {
			for i := range vv {
				vv[i] = "[MASKED]"
			}

			masked = true
		}
	}

	if !masked {
		return raw
	}

	return values.Encode()
}

// isSensitive reports whether a field or parameter name is in sensitive or contains a sensitiveHints
// entry, after normalizeFieldName.
func isSensitive(name string, sensitive map[string]struct{}) bool {
	normalized := normalizeFieldName(name)
	if _, ok := sensitive[normalized]; ok {
		return true
	}

	for _, hint := range sensitiveHints {
		if strings.Contains(normalized, hint) {
			return true
		}
	}

	return false
}

// fieldNameReplacer drops the separators normalizeFieldName ignores.
var fieldNameReplacer = strings.NewReplacer("_", "", "-", "")

// normalizeFieldName folds case and drops "_"/"-" so access_token, accessToken and Access-Token match.
func normalizeFieldName(name string) string {
	return fieldNameReplacer.Replace(strings.ToLower(name))
}
