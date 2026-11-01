package middleware_test

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

	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/response"
)

// newAccessLogRouter creates a test Gin router with AccessLog and observed logs.
func newAccessLogRouter(opts middleware.AccessLogOptions, handler gin.HandlerFunc) (*gin.Engine, *observer.ObservedLogs) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(response.TraceIDContextKey, "trace-123")
		c.Next()
	})
	router.Use(middleware.AccessLog(logger, opts))
	router.GET("/ping", handler)
	router.POST("/login", handler)
	router.GET("/health", handler)

	return router, logs
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

	all := logs.All()
	if len(all) != 1 {
		t.Fatalf("got %d log entries, want 1", len(all))
	}

	reqPayload, ok := all[0].ContextMap()["request_payload"].(map[string]any)
	if !ok {
		t.Fatalf("request_payload = %#v, want a map", all[0].ContextMap()["request_payload"])
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

	reqPayload := logs.All()[0].ContextMap()["request_payload"].(map[string]any)
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
	router, logs := newAccessLogRouter(middleware.AccessLogOptions{}, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping?page=2&reset_token=abc", nil))
	if got := logs.All()[0].ContextMap()["query"]; got != "page=2&reset_token=%5BMASKED%5D" {
		t.Errorf("query = %v, want the token value masked", got)
	}

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping?b=1&a=2", nil))
	if got := logs.All()[1].ContextMap()["query"]; got != "b=1&a=2" {
		t.Errorf("query = %v, want it unchanged when nothing is sensitive", got)
	}
}

func TestAccessLog_BodiesOnErrorOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)

	router := gin.New()
	router.Use(middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder()))
	router.Use(middleware.AccessLog(zap.New(core), middleware.AccessLogOptions{BodiesOnErrorOnly: true}))
	router.POST("/ok", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"email": "a@b.com"}) })
	router.POST("/bad", func(c *gin.Context) { c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid"}) })

	for _, path := range []string{"/ok", "/bad"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"a@b.com"}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(httptest.NewRecorder(), req)
	}

	ok, bad := logs.All()[0].ContextMap(), logs.All()[1].ContextMap()
	if ok["request_payload"] != nil || ok["response_payload"] != nil {
		t.Errorf("2xx payloads = %v / %v, want both left out", ok["request_payload"], ok["response_payload"])
	}

	if bad["request_payload"] == nil || bad["response_payload"] == nil {
		t.Errorf("4xx payloads = %v / %v, want both logged", bad["request_payload"], bad["response_payload"])
	}
}

// newAccessLogRouterWithPayload wires Payload before AccessLog for payload testing.
func newAccessLogRouterWithPayload(t *testing.T) (*gin.Engine, *observer.ObservedLogs) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	router := gin.New()
	router.Use(middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder()))
	router.Use(middleware.AccessLog(logger, middleware.AccessLogOptions{}))
	router.POST("/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return router, logs
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
		t.Fatalf("got %d log entries total, want still 1 — /ping is excluded by the custom list", len(logs.All()))
	}
}

func TestAccessLog_MasksNestedFieldsInTopLevelArray(t *testing.T) {
	router, logs := newAccessLogRouter(middleware.AccessLogOptions{}, func(c *gin.Context) {
		c.JSON(http.StatusOK, []any{gin.H{"items": []any{[]any{gin.H{"accessToken": "t1", "API-Key": "k1", "name": "ok"}}}}})
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ping", nil))

	got := fmt.Sprint(logs.All()[0].ContextMap()["response_payload"])
	if strings.Contains(got, "t1") || strings.Contains(got, "k1") || !strings.Contains(got, "name:ok") {
		t.Fatalf("response_payload = %s, want accessToken and API-Key masked, name kept", got)
	}
}

func TestAccessLog_RespSizeIsUncompressedInsideGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zapcore.InfoLevel)
	body := strings.Repeat("a", 1000)

	router := gin.New()
	router.Use(middleware.Gzip(middleware.GzipOptions{}), middleware.AccessLog(zap.New(core), middleware.AccessLogOptions{}))
	router.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, body) })

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	router.ServeHTTP(httptest.NewRecorder(), req)

	if got := logs.All()[0].ContextMap()["resp_size"]; got != int64(len(body)) {
		t.Fatalf("resp_size = %v, want %d (uncompressed)", got, len(body))
	}
}
