package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: route_patterns_test.go
// Purpose: Verify RoutePatterns answers 404 when a named path parameter does not match its pattern.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 13
//
// #   Test Case                                          Scenario                                Expected Result
// 1   TestRoutePatterns_RejectsParamsNotMatchingPattern  :uuid is a hyphenated UUID              200
// 2   TestRoutePatterns_RejectsParamsNotMatchingPattern  :uuid is plain text                     404
// 3   TestRoutePatterns_RejectsParamsNotMatchingPattern  :uuid without hyphens                   404
// 4   TestRoutePatterns_RejectsParamsNotMatchingPattern  :id is a positive integer               200
// 5   TestRoutePatterns_RejectsParamsNotMatchingPattern  :id is zero                             404
// 6   TestRoutePatterns_RejectsParamsNotMatchingPattern  :id is negative                         404
// 7   TestRoutePatterns_RejectsParamsNotMatchingPattern  :id overflows int64                     404
// 8   TestRoutePatterns_RejectsParamsNotMatchingPattern  :id mixes digits and letters            404
// 9   TestRoutePatterns_RejectsParamsNotMatchingPattern  :slug lowercase with single hyphens     200
// 10  TestRoutePatterns_RejectsParamsNotMatchingPattern  :slug with uppercase and underscore     404
// 11  TestRoutePatterns_RejectsParamsNotMatchingPattern  :slug with a double hyphen              404
// 12  TestRoutePatterns_RejectsParamsNotMatchingPattern  Valid :slug but :id is not a number     404
// 13  TestRoutePatterns_RejectsParamsNotMatchingPattern  Parameter without a registered pattern  200 for any value
//
// =============================================================================

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
)

func TestRoutePatterns_RejectsParamsNotMatchingPattern(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.RoutePatterns(map[string]func(string) bool{
		"uuid": middleware.IsUUID,
		"id":   middleware.IsID,
		"slug": middleware.IsSlug,
	}, newResponder()))

	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	router.GET("/users/:uuid", ok)
	router.GET("/orders/:id", ok)
	router.GET("/posts/:slug/comments/:id", ok)
	router.GET("/files/:key", ok)

	tests := []struct {
		name string
		path string
		want int
	}{
		{"uuid is a hyphenated uuid", "/users/9b2f6c1e-3a4d-4f5e-8a7b-1c2d3e4f5a6b", http.StatusOK},
		{"uuid is plain text", "/users/abc", http.StatusNotFound},
		{"uuid without hyphens", "/users/9b2f6c1e3a4d4f5e8a7b1c2d3e4f5a6b", http.StatusNotFound},
		{"id is a positive integer", "/orders/42", http.StatusOK},
		{"id is zero", "/orders/0", http.StatusNotFound},
		{"id is negative", "/orders/-1", http.StatusNotFound},
		{"id overflows int64", "/orders/99999999999999999999", http.StatusNotFound},
		{"id mixes digits and letters", "/orders/12abc", http.StatusNotFound},
		{"slug lowercase with single hyphens", "/posts/hello-world-2/comments/7", http.StatusOK},
		{"slug with uppercase and underscore", "/posts/Hello_World/comments/7", http.StatusNotFound},
		{"slug with a double hyphen", "/posts/hello--world/comments/7", http.StatusNotFound},
		{"valid slug but id is not a number", "/posts/hello/comments/x", http.StatusNotFound},
		{"parameter without a registered pattern", "/files/anything_GOES.txt", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if w.Code != tt.want {
				t.Fatalf("GET %s status = %d, want %d", tt.path, w.Code, tt.want)
			}
		})
	}
}
