package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
)

func newCORSContext(method, origin string) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/", nil)
	if origin != "" {
		c.Request.Header.Set("Origin", origin)
	}

	return w, c
}

func TestCORS_AllowedSpecificOrigin(t *testing.T) {
	opts := middleware.CORSOptions{
		AllowOrigins: []string{"https://app.example.com"},
		AllowMethods: []string{"GET", "POST"},
		AllowHeaders: []string{"Authorization"},
	}
	w, c := newCORSContext(http.MethodGet, "https://app.example.com")

	middleware.CORS(opts)(c)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
	}

	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want %q", got, "Origin")
	}

	if got := w.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Errorf("Access-Control-Allow-Methods = %q, want %q", got, "GET, POST")
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"https://app.example.com"}}
	w, c := newCORSContext(http.MethodGet, "https://evil.example.com")

	middleware.CORS(opts)(c)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestCORS_WildcardWithoutCredentials(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"*"}}
	w, c := newCORSContext(http.MethodGet, "https://anywhere.example.com")

	middleware.CORS(opts)(c)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

func TestCORS_WildcardWithCredentialsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("CORS did not panic for AllowOrigins \"*\" with AllowCredentials")
		}
	}()

	middleware.CORS(middleware.CORSOptions{AllowOrigins: []string{"*"}, AllowCredentials: true})
}

func TestCORS_NoOriginHeader(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"https://app.example.com"}}
	w, c := newCORSContext(http.MethodGet, "")

	middleware.CORS(opts)(c)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty when no Origin header is sent", got)
	}
}

func TestCORS_PreflightIsAborted(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"*"}}
	w, c := newCORSContext(http.MethodOptions, "https://app.example.com")

	middleware.CORS(opts)(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d for an OPTIONS preflight", w.Code, http.StatusNoContent)
	}

	if !c.IsAborted() {
		t.Error("a preflight OPTIONS request should abort the chain")
	}
}

func TestCORS_NonPreflightContinuesTheChain(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"*"}}
	w, c := newCORSContext(http.MethodGet, "https://app.example.com")

	middleware.CORS(opts)(c)

	if c.IsAborted() {
		t.Error("a normal GET request should not abort the chain")
	}

	_ = w
}

func TestCORS_VaryOriginOnDisallowedOrMissingOrigin(t *testing.T) {
	for _, origin := range []string{"https://evil.example.com", ""} {
		w, c := newCORSContext(http.MethodGet, origin)
		c.Writer.Header().Add("Vary", "Accept-Encoding")

		middleware.CORS(middleware.CORSOptions{AllowOrigins: []string{"https://app.example.com"}})(c)

		if got := w.Header().Values("Vary"); len(got) != 2 || got[1] != "Origin" {
			t.Errorf("origin %q: Vary = %v, want [Accept-Encoding Origin]", origin, got)
		}
	}
}
