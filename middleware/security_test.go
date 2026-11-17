package middleware_test

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
)

func TestSecurity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("standard headers are always set", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		middleware.Security(middleware.SecurityOptions{})(c)

		tests := map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "DENY",
			"Referrer-Policy":              "strict-origin-when-cross-origin",
			"Cross-Origin-Opener-Policy":   "same-origin-allow-popups",
			"Cross-Origin-Resource-Policy": "same-site",
			"Permissions-Policy":           "camera=(), microphone=(), geolocation=()",
		}
		for header, want := range tests {
			if got := w.Header().Get(header); got != want {
				t.Errorf("%s = %q, want %q", header, got, want)
			}
		}
	})

	t.Run("HSTS is only set over TLS", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		middleware.Security(middleware.SecurityOptions{})(c)

		if got := w.Header().Get("Strict-Transport-Security"); got != "" {
			t.Errorf("Strict-Transport-Security = %q over plain HTTP, want empty", got)
		}
	})

	t.Run("HSTS is set when the request came over TLS", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.TLS = &tls.ConnectionState{}

		middleware.Security(middleware.SecurityOptions{})(c)

		if got := w.Header().Get("Strict-Transport-Security"); got == "" {
			t.Error("Strict-Transport-Security should be set for a TLS request")
		}
	})

	t.Run("ForceHSTS and CORP are configurable", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		middleware.Security(middleware.SecurityOptions{ForceHSTS: true, CrossOriginResourcePolicy: "cross-origin"})(c)

		if w.Header().Get("Strict-Transport-Security") == "" || w.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
			t.Errorf("headers = %v, want HSTS without TLS and CORP cross-origin", w.Header())
		}
	})
}
