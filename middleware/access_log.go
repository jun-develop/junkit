package middleware

import (
	"bytes"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/security"
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
	// token and authorization. Names security.IsSecretName flags are always masked.
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

// withDefaults fills unset fields.
func (o AccessLogOptions) withDefaults() AccessLogOptions {
	if len(o.ExcludedPaths) == 0 {
		o.ExcludedPaths = defaultExcludedPaths
	}

	if len(o.SensitiveFields) == 0 {
		o.SensitiveFields = defaultSensitiveFields
	}

	if o.MaxBodySize <= 0 {
		o.MaxBodySize = defaultMaxAccessLogBodySize
	}

	return o
}

// AccessLog returns a middleware that logs structured HTTP request/response details. The response
// body is logged as text, truncated to MaxBodySize, with sensitive JSON string fields masked.
//
// Usage:
//
//	router.Use(middleware.AccessLog(middleware.AccessLogOptions{}, httpLogger))
func AccessLog(opts AccessLogOptions, httpLogger *zap.Logger) gin.HandlerFunc {
	opts = opts.withDefaults()
	excluded := toSet(opts.ExcludedPaths)
	sensitive := make(map[string]struct{}, len(opts.SensitiveFields))
	for _, f := range opts.SensitiveFields {
		sensitive[normalizeFieldName(f)] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, ok := excluded[c.Request.URL.Path]; ok {
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

		c.Writer = &accessLogResponseWriter{ResponseWriter: c.Writer, body: respBuf, maxSize: opts.MaxBodySize}

		c.Next()

		status := c.Writer.Status()
		var reqBody any
		var respBody string
		if !opts.BodiesOnErrorOnly || status >= 400 {
			if payload := GetPayload(c); payload != nil {
				sanitizeInPlace(payload, sensitive)
				reqBody = payload
			}

			respBody = maskJSONFields(respBuf.String(), sensitive)
		}

		var errs any
		if len(c.Errors) > 0 {
			errs = c.Errors.ByType(gin.ErrorTypePrivate).String()
		}

		accessLogFuncForStatus(httpLogger, status)("HTTP_ACCESS_LOG",
			zap.String("trace_id", logger.GetTraceID(c.Request.Context())),
			zap.String("hostname", accessLogHostname),
			zap.Int64("latency_ms", time.Since(start).Milliseconds()),
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("query", maskQuery(c.Request.URL.RawQuery, sensitive)),
			zap.String("ip", c.ClientIP()),
			zap.String("user_agent", c.Request.UserAgent()),
			zap.Int("resp_size", c.Writer.Size()),
			zap.Any("error", errs),
			zap.Any("request_payload", reqBody),
			zap.String("response_payload", respBody),
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

// jsonStringField matches a JSON "key": "string value" pair, capturing the key.
var jsonStringField = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"(\s*:\s*)"(?:[^"\\]|\\.)*"`)

// maskJSONFields masks the string values of sensitive keys in body, which may be truncated JSON or
// any text; it avoids parsing the whole body just to log it.
func maskJSONFields(body string, sensitive map[string]struct{}) string {
	return jsonStringField.ReplaceAllStringFunc(body, func(m string) string {
		sub := jsonStringField.FindStringSubmatch(m)
		if !isSensitive(sub[1], sensitive) {
			return m
		}

		return `"` + sub[1] + `"` + sub[2] + `"[MASKED]"`
	})
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

// isSensitive reports whether a field or parameter name, after normalizeFieldName, is in sensitive
// or flagged by security.IsSecretName.
func isSensitive(name string, sensitive map[string]struct{}) bool {
	normalized := normalizeFieldName(name)
	if _, ok := sensitive[normalized]; ok {
		return true
	}

	return security.IsSecretName(normalized)
}

// fieldNameReplacer drops the separators normalizeFieldName ignores.
var fieldNameReplacer = strings.NewReplacer("_", "", "-", "")

// normalizeFieldName folds case and drops "_"/"-" so access_token, accessToken and Access-Token match.
func normalizeFieldName(name string) string {
	return fieldNameReplacer.Replace(strings.ToLower(name))
}
