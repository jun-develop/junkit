package middleware_test

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

	if got, ok := security.GetIdentity[*testIdentity](c.Request.Context()); !ok || got != want {
		t.Errorf("from request context: got %v, %v", got, ok)
	}

	if got, ok := security.GetIdentity[*testIdentity](c); !ok || got != want {
		t.Errorf("from gin.Context with ContextWithFallback: got %v, %v", got, ok)
	}
}
