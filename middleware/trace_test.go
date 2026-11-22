package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: trace_test.go
// Purpose: Verify the Trace middleware assigns a safe trace ID and stores build metadata.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 7
//
// #  Test Case                                    Scenario                              Expected Result
// 1  TestTrace_GeneratesATraceIDWhenNoneProvided  No incoming trace ID                  Response header set and equal to the ID in the request context
// 2  TestTrace_IncomingTraceIDOnlyWhenTrusted     Incoming ID, TrustHeader true         Incoming ID reused in header and context
// 3  TestTrace_IncomingTraceIDOnlyWhenTrusted     Incoming ID, TrustHeader false        New ID generated, header and context match
// 4  TestTrace_ReplacesAnUnsafeIncomingTraceID    Trusted ID containing HTML markup     Replaced by a generated UUID
// 5  TestTrace_ReplacesAnUnsafeIncomingTraceID    Trusted ID containing spaces          Replaced by a generated UUID
// 6  TestTrace_ReplacesAnUnsafeIncomingTraceID    Trusted ID longer than 64 characters  Replaced by a generated UUID
// 7  TestTrace_SetsBuildMetadataInContext         Version and Environment configured    Both stored in the gin context
//
// =============================================================================

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/response"
)

func TestTrace_GeneratesATraceIDWhenNoneProvided(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	middleware.Trace(middleware.TraceOptions{Version: "v1.0.0"})(c)

	traceID := w.Header().Get(middleware.HeaderTraceID)
	if traceID == "" {
		t.Fatal("Trace() did not set a response trace ID header")
	}

	if got := logger.GetTraceID(c.Request.Context()); got != traceID {
		t.Fatalf("logger.GetTraceID(request context) = %q, want it to match the response header %q", got, traceID)
	}
}

func TestTrace_IncomingTraceIDOnlyWhenTrusted(t *testing.T) {
	tests := []struct {
		name       string
		trust      bool
		wantReused bool
	}{
		{"trusted header is reused", true, true},
		{"untrusted header is replaced", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Request.Header.Set(middleware.HeaderTraceID, "incoming-trace-id")

			middleware.Trace(middleware.TraceOptions{TrustHeader: tt.trust})(c)

			got := w.Header().Get(middleware.HeaderTraceID)
			if reused := got == "incoming-trace-id"; reused != tt.wantReused {
				t.Fatalf("response trace ID = %q, incoming ID reused = %v, want %v", got, reused, tt.wantReused)
			}

			if ctxID := logger.GetTraceID(c.Request.Context()); ctxID != got {
				t.Fatalf("logger.GetTraceID(request context) = %q, want it to match the response header %q", ctxID, got)
			}
		})
	}
}

func TestTrace_ReplacesAnUnsafeIncomingTraceID(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
	}{
		{"contains html markup", `</pre><a href="https://example.invalid">x</a>`},
		{"contains spaces", "trace id"},
		{"longer than 64 characters", strings.Repeat("a", 65)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Request.Header.Set(middleware.HeaderTraceID, tt.incoming)

			middleware.Trace(middleware.TraceOptions{TrustHeader: true})(c)

			got := w.Header().Get(middleware.HeaderTraceID)
			if got == tt.incoming {
				t.Fatalf("response trace ID = %q, want a generated ID instead of the unsafe incoming one", got)
			}

			if _, err := uuid.Parse(got); err != nil {
				t.Fatalf("response trace ID = %q, want a generated UUID: %v", got, err)
			}
		})
	}
}

func TestTrace_SetsBuildMetadataInContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	middleware.Trace(middleware.TraceOptions{Version: "v1.2.3", Environment: "production"})(c)

	if v, _ := c.Get(response.VersionContextKey); v != "v1.2.3" {
		t.Fatalf("gin context %q = %v, want %q", response.VersionContextKey, v, "v1.2.3")
	}

	if v, _ := c.Get(response.EnvContextKey); v != "production" {
		t.Fatalf("gin context %q = %v, want %q", response.EnvContextKey, v, "production")
	}
}
