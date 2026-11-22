package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: identity_test.go
// Purpose: Verify that SetIdentity stores the caller identity where handlers can read it.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 1
//
// #  Test Case                                                        Scenario                                                Expected Result
// 1  TestSetIdentity_ReadableFromRequestContextAndFallbackGinContext  Identity set on a gin.Context with ContextWithFallback  Readable from the request context and from the gin.Context
//
// =============================================================================

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/security"
)

type testIdentity struct{ UserID int64 }

func TestSetIdentity_ReadableFromRequestContextAndFallbackGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, engine := gin.CreateTestContext(httptest.NewRecorder())
	engine.ContextWithFallback = true
	c.Request = httptest.NewRequest("GET", "/", nil)
	want := &testIdentity{UserID: 42}

	middleware.SetIdentity(c, want)

	got, ok := security.GetIdentity[*testIdentity](c.Request.Context())
	if !ok {
		t.Error("GetIdentity(request context) ok = false, want true")
	}

	if got != want {
		t.Errorf("GetIdentity(request context) = %v, want %v", got, want)
	}

	got, ok = security.GetIdentity[*testIdentity](c)
	if !ok {
		t.Error("GetIdentity(gin.Context with ContextWithFallback) ok = false, want true")
	}

	if got != want {
		t.Errorf("GetIdentity(gin.Context with ContextWithFallback) = %v, want %v", got, want)
	}
}
