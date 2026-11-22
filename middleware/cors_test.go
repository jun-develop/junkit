package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: cors_test.go
// Purpose: Verify the CORS middleware headers, preflight handling and invalid configuration.
// Type: Unit
//
// Test functions: 8
// Total scenarios: 9
//
// #  Test Case                                       Scenario                                 Expected Result
// 1  TestCORS_EchoesAllowedOrigin                    Origin is in AllowOrigins                Origin echoed, Vary: Origin, allowed methods listed
// 2  TestCORS_OmitsHeaderForDisallowedOrigin         Origin not in AllowOrigins               No Access-Control-Allow-Origin header
// 3  TestCORS_AllowsAnyOriginWithWildcard            AllowOrigins "*" without credentials     Access-Control-Allow-Origin is "*"
// 4  TestCORS_PanicsOnWildcardWithCredentials        AllowOrigins "*" with AllowCredentials   CORS panics at setup
// 5  TestCORS_OmitsHeaderWithoutOriginHeader         Request without an Origin header         No Access-Control-Allow-Origin header
// 6  TestCORS_PreflightIsAborted                     OPTIONS preflight                        204 and the chain is aborted
// 7  TestCORS_NonPreflightContinuesTheChain          Normal GET                               Chain is not aborted
// 8  TestCORS_VaryOriginOnDisallowedOrMissingOrigin  Disallowed origin, existing Vary header  Vary is [Accept-Encoding Origin]
// 9  TestCORS_VaryOriginOnDisallowedOrMissingOrigin  No Origin header, existing Vary header   Vary is [Accept-Encoding Origin]
//
// =============================================================================

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
)

// newCORSContext returns a test context for a request to "/" with the given Origin (none if empty).
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

func TestCORS_EchoesAllowedOrigin(t *testing.T) {
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

func TestCORS_OmitsHeaderForDisallowedOrigin(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"https://app.example.com"}}
	w, c := newCORSContext(http.MethodGet, "https://evil.example.com")

	middleware.CORS(opts)(c)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestCORS_AllowsAnyOriginWithWildcard(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"*"}}
	w, c := newCORSContext(http.MethodGet, "https://anywhere.example.com")

	middleware.CORS(opts)(c)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "*")
	}
}

func TestCORS_PanicsOnWildcardWithCredentials(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("CORS() did not panic for AllowOrigins \"*\" with AllowCredentials")
		}
	}()

	middleware.CORS(middleware.CORSOptions{AllowOrigins: []string{"*"}, AllowCredentials: true})
}

func TestCORS_OmitsHeaderWithoutOriginHeader(t *testing.T) {
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
		t.Error("chain not aborted, want a preflight OPTIONS request to abort it")
	}
}

func TestCORS_NonPreflightContinuesTheChain(t *testing.T) {
	opts := middleware.CORSOptions{AllowOrigins: []string{"*"}}
	_, c := newCORSContext(http.MethodGet, "https://app.example.com")

	middleware.CORS(opts)(c)

	if c.IsAborted() {
		t.Error("chain aborted, want a normal GET request to continue")
	}
}

func TestCORS_VaryOriginOnDisallowedOrMissingOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
	}{
		{"disallowed origin", "https://evil.example.com"},
		{"no origin header", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, c := newCORSContext(http.MethodGet, tt.origin)
			c.Writer.Header().Add("Vary", "Accept-Encoding")

			middleware.CORS(middleware.CORSOptions{AllowOrigins: []string{"https://app.example.com"}})(c)

			if got := w.Header().Values("Vary"); len(got) != 2 || got[1] != "Origin" {
				t.Errorf("Vary = %v, want [Accept-Encoding Origin]", got)
			}
		})
	}
}
