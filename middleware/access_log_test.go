package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: access_log_test.go
// Purpose: Verify the AccessLog middleware: one structured entry per request, severity, masking and exclusions.
// Type: Unit
//
// Test functions: 11
// Total scenarios: 14
//
// #   Test Case                                          Scenario                                     Expected Result
// 1   TestAccessLog_LogsOneEntryPerRequest               GET /ping returns 200                        One HTTP_ACCESS_LOG Info entry with status, method, path, trace_id
// 2   TestAccessLog_SeverityMatchesStatusCode            2xx response                                 Logged at Info
// 3   TestAccessLog_SeverityMatchesStatusCode            4xx response                                 Logged at Warn
// 4   TestAccessLog_SeverityMatchesStatusCode            5xx response                                 Logged at Error
// 5   TestAccessLog_MasksSensitiveRequestFields          JSON body with password and email            password masked, email kept
// 6   TestAccessLog_MasksFieldsNamedLikeCredentials      Fields named like password/token/secret      Those fields masked, name kept
// 7   TestAccessLog_MasksSensitiveQueryParameters        Query with reset_token                       Token value masked, page kept
// 8   TestAccessLog_MasksSensitiveQueryParameters        Query without secrets                        Query logged unchanged, order kept
// 9   TestAccessLog_BodiesOnErrorOnly                    BodiesOnErrorOnly, a 2xx then a 4xx request  Bodies left out for 2xx, logged for 4xx
// 10  TestAccessLog_ExcludedPathIsNotLogged              GET /health (excluded by default)            No log entry
// 11  TestAccessLog_SkipIfBypassesLogging                SkipIf returns true                          No log entry
// 12  TestAccessLog_CustomExcludedPathsOverrideDefaults  ExcludedPaths = [/ping]                      /health is logged, /ping is not
// 13  TestAccessLog_MasksNestedFieldsInTopLevelArray     Secrets nested in arrays of the response     accessToken and API-Key masked, name kept
// 14  TestAccessLog_RespSizeIsUncompressedInsideGzip     1000-byte body behind Gzip                   resp_size is the uncompressed 1000
//
// =============================================================================

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/middleware"
)

// newAccessLogRouter returns a router with AccessLog (trace ID "trace-123"), routes /ping, /login
// and /health served by handler, and the observed log entries.
func newAccessLogRouter(opts middleware.AccessLogOptions, handler gin.HandlerFunc) (*gin.Engine, *observer.ObservedLogs) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)
	log := zap.New(core)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(logger.WithTraceID(c.Request.Context(), "trace-123"))
		c.Next()
	})
	router.Use(middleware.AccessLog(opts, log))
	router.GET("/ping", handler)
	router.POST("/login", handler)
	router.GET("/health", handler)

	return router, logs
}

// newAccessLogRouterWithPayload returns a router with Payload before AccessLog, so request bodies
// are logged, and a POST /login route answering 200.
func newAccessLogRouterWithPayload(t *testing.T) (*gin.Engine, *observer.ObservedLogs) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)

	router := gin.New()
	router.Use(middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder()))
	router.Use(middleware.AccessLog(middleware.AccessLogOptions{}, zap.New(core)))
	router.POST("/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return router, logs
}

// onlyEntryFields fails unless exactly one entry was logged and returns its fields.
func onlyEntryFields(t *testing.T, logs *observer.ObservedLogs) map[string]any {
	t.Helper()
	all := logs.All()
	if len(all) != 1 {
		t.Fatalf("got %d log entries, want 1", len(all))
	}

	return all[0].ContextMap()
}

func TestAccessLog_LogsOneEntryPerRequest(t *testing.T) {
	router, logs := newAccessLogRouter(middleware.AccessLogOptions{}, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	all := logs.All()
	if len(all) != 1 {
		t.Fatalf("got %d log entries, want 1", len(all))
	}

	entry := all[0]
	if entry.Message != "HTTP_ACCESS_LOG" {
		t.Errorf("message = %q, want %q", entry.Message, "HTTP_ACCESS_LOG")
	}

	if entry.Level != zapcore.InfoLevel {
		t.Errorf("level = %v, want Info for a 2xx status", entry.Level)
	}

	fields := entry.ContextMap()
	if fields["status"] != int64(http.StatusOK) {
		t.Errorf("status field = %v, want %d", fields["status"], http.StatusOK)
	}

	if fields["method"] != http.MethodGet {
		t.Errorf("method field = %v, want %q", fields["method"], http.MethodGet)
	}

	if fields["path"] != "/ping" {
		t.Errorf("path field = %v, want %q", fields["path"], "/ping")
	}

	if fields["trace_id"] != "trace-123" {
		t.Errorf("trace_id field = %v, want %q", fields["trace_id"], "trace-123")
	}
}

func TestAccessLog_SeverityMatchesStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantLevel  zapcore.Level
	}{
		{name: "2xx is Info", statusCode: http.StatusOK, wantLevel: zapcore.InfoLevel},
		{name: "4xx is Warn", statusCode: http.StatusBadRequest, wantLevel: zapcore.WarnLevel},
		{name: "5xx is Error", statusCode: http.StatusInternalServerError, wantLevel: zapcore.ErrorLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, logs := newAccessLogRouter(middleware.AccessLogOptions{}, func(c *gin.Context) {
				c.Status(tt.statusCode)
			})

			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

			all := logs.All()
			if len(all) != 1 {
				t.Fatalf("got %d log entries, want 1", len(all))
			}

			if all[0].Level != tt.wantLevel {
				t.Errorf("level = %v, want %v for status %d", all[0].Level, tt.wantLevel, tt.statusCode)
			}
		})
	}
}

func TestAccessLog_MasksSensitiveRequestFields(t *testing.T) {
	router, logs := newAccessLogRouterWithPayload(t)
	body := `{"email":"a@b.com","password":"super-secret"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(httptest.NewRecorder(), req)

	fields := onlyEntryFields(t, logs)
	reqPayload, ok := fields["request_payload"].(map[string]any)
	if !ok {
		t.Fatalf("request_payload = %#v, want a map", fields["request_payload"])
	}

	if reqPayload["password"] != "[MASKED]" {
		t.Errorf("password field = %v, want it masked", reqPayload["password"])
	}

	if reqPayload["email"] != "a@b.com" {
		t.Errorf("email field = %v, want it untouched", reqPayload["email"])
	}
}

func TestAccessLog_MasksFieldsNamedLikeCredentials(t *testing.T) {
	router, logs := newAccessLogRouterWithPayload(t)
	body := `{"password_confirmation":"p1","reset_token":"t1","clientSecretKey":"s1","name":"Jane"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(httptest.NewRecorder(), req)

	fields := onlyEntryFields(t, logs)
	reqPayload, ok := fields["request_payload"].(map[string]any)
	if !ok {
		t.Fatalf("request_payload = %#v, want a map", fields["request_payload"])
	}

	for _, field := range []string{"password_confirmation", "reset_token", "clientSecretKey"} {
		if reqPayload[field] != "[MASKED]" {
			t.Errorf("%s = %v, want it masked", field, reqPayload[field])
		}
	}

	if reqPayload["name"] != "Jane" {
		t.Errorf("name = %v, want it untouched", reqPayload["name"])
	}
}

func TestAccessLog_MasksSensitiveQueryParameters(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{"query with reset_token", "/ping?page=2&reset_token=abc", "page=2&reset_token=%5BMASKED%5D"},
		{"query without secrets", "/ping?b=1&a=2", "b=1&a=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, logs := newAccessLogRouter(middleware.AccessLogOptions{}, func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tt.target, nil))

			if got := onlyEntryFields(t, logs)["query"]; got != tt.want {
				t.Errorf("query = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestAccessLog_BodiesOnErrorOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)

	router := gin.New()
	router.Use(middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder()))
	router.Use(middleware.AccessLog(middleware.AccessLogOptions{BodiesOnErrorOnly: true}, zap.New(core)))
	router.POST("/ok", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"email": "a@b.com"}) })
	router.POST("/bad", func(c *gin.Context) { c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid"}) })

	for _, path := range []string{"/ok", "/bad"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"a@b.com"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(httptest.NewRecorder(), req)
	}

	all := logs.All()
	if len(all) != 2 {
		t.Fatalf("got %d log entries, want 2", len(all))
	}

	ok, bad := all[0].ContextMap(), all[1].ContextMap()
	if ok["request_payload"] != nil {
		t.Errorf("2xx request_payload = %v, want it left out", ok["request_payload"])
	}

	if ok["response_payload"] != "" {
		t.Errorf("2xx response_payload = %v, want it left out", ok["response_payload"])
	}

	if bad["request_payload"] == nil {
		t.Error("4xx request_payload = nil, want it logged")
	}

	if bad["response_payload"] == "" {
		t.Error("4xx response_payload is empty, want it logged")
	}
}

func TestAccessLog_ExcludedPathIsNotLogged(t *testing.T) {
	router, logs := newAccessLogRouter(middleware.AccessLogOptions{}, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if len(logs.All()) != 0 {
		t.Fatalf("got %d log entries for an excluded path, want 0", len(logs.All()))
	}
}

func TestAccessLog_SkipIfBypassesLogging(t *testing.T) {
	opts := middleware.AccessLogOptions{
		SkipIf: func(c *gin.Context) bool { return c.Query("t_debug") == "1234" },
	}
	router, logs := newAccessLogRouter(opts, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping?t_debug=1234", nil))

	if len(logs.All()) != 0 {
		t.Fatalf("got %d log entries, want 0 when SkipIf returns true", len(logs.All()))
	}
}

func TestAccessLog_CustomExcludedPathsOverrideDefaults(t *testing.T) {
	opts := middleware.AccessLogOptions{ExcludedPaths: []string{"/ping"}}
	router, logs := newAccessLogRouter(opts, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	if len(logs.All()) != 1 {
		t.Fatalf("got %d log entries for /health, want 1 since ExcludedPaths was overridden", len(logs.All()))
	}

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))
	if len(logs.All()) != 1 {
		t.Fatalf("got %d log entries total, want still 1 because /ping is excluded by the custom list", len(logs.All()))
	}
}

func TestAccessLog_MasksNestedFieldsInTopLevelArray(t *testing.T) {
	router, logs := newAccessLogRouter(middleware.AccessLogOptions{}, func(c *gin.Context) {
		c.JSON(http.StatusOK, []any{gin.H{"items": []any{[]any{gin.H{"accessToken": "t1", "API-Key": "k1", "name": "ok"}}}}})
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	got := fmt.Sprint(onlyEntryFields(t, logs)["response_payload"])
	if strings.Contains(got, "t1") {
		t.Errorf("response_payload = %s, want accessToken masked", got)
	}

	if strings.Contains(got, "k1") {
		t.Errorf("response_payload = %s, want API-Key masked", got)
	}

	if !strings.Contains(got, `"name":"ok"`) {
		t.Errorf("response_payload = %s, want name kept", got)
	}
}

func TestAccessLog_RespSizeIsUncompressedInsideGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)
	body := strings.Repeat("a", 1000)

	router := gin.New()
	router.Use(middleware.Gzip(middleware.GzipOptions{}), middleware.AccessLog(middleware.AccessLogOptions{}, zap.New(core)))
	router.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, body) })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	router.ServeHTTP(httptest.NewRecorder(), req)

	if got := onlyEntryFields(t, logs)["resp_size"]; got != int64(len(body)) {
		t.Fatalf("resp_size = %v, want %d (uncompressed)", got, len(body))
	}
}
