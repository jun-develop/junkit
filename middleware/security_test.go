package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: security_test.go
// Purpose: Verify the Security middleware sets the standard security headers and HSTS rules.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 4
//
// #  Test Case                         Scenario                     Expected Result
// 1  TestSecurity_SetsSecurityHeaders  Default options              nosniff, DENY, referrer, COOP, CORP and permissions headers set
// 2  TestSecurity_SetsSecurityHeaders  Plain HTTP request           No Strict-Transport-Security header
// 3  TestSecurity_SetsSecurityHeaders  Request over TLS             Strict-Transport-Security set
// 4  TestSecurity_SetsSecurityHeaders  ForceHSTS and a custom CORP  HSTS set without TLS, CORP is cross-origin
//
// =============================================================================

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
)

func TestSecurity_SetsSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("default options", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		middleware.Security(middleware.SecurityOptions{})(c)

		wantHeaders := []struct{ name, value string }{
			{"X-Content-Type-Options", "nosniff"},
			{"X-Frame-Options", "DENY"},
			{"Referrer-Policy", "strict-origin-when-cross-origin"},
			{"Cross-Origin-Opener-Policy", "same-origin-allow-popups"},
			{"Cross-Origin-Resource-Policy", "same-site"},
			{"Permissions-Policy", "camera=(), microphone=(), geolocation=()"},
		}
		for _, h := range wantHeaders {
			if got := w.Header().Get(h.name); got != h.value {
				t.Errorf("%s = %q, want %q", h.name, got, h.value)
			}
		}
	})

	t.Run("plain http request has no hsts", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		middleware.Security(middleware.SecurityOptions{})(c)

		if got := w.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("Strict-Transport-Security = %q over plain HTTP, want empty", got)
		}
	})

	t.Run("request over tls gets hsts", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.TLS = &tls.ConnectionState{}

		middleware.Security(middleware.SecurityOptions{})(c)

		if got := w.Header().Get("Strict-Transport-Security"); got == "" {
			t.Error("Strict-Transport-Security is empty, want it set for a TLS request")
		}
	})

	t.Run("force hsts and custom corp", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		middleware.Security(middleware.SecurityOptions{ForceHSTS: true, CrossOriginResourcePolicy: "cross-origin"})(c)

		if got := w.Header().Get("Strict-Transport-Security"); got == "" {
			t.Error("Strict-Transport-Security is empty, want it set by ForceHSTS without TLS")
		}

		if got := w.Header().Get("Cross-Origin-Resource-Policy"); got != "cross-origin" {
			t.Errorf("Cross-Origin-Resource-Policy = %q, want cross-origin", got)
		}
	})
}
