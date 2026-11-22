package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: payload_test.go
// Purpose: Verify the Payload middleware: body caching by content type, size limits and read failures.
// Type: Unit
//
// Test functions: 9
// Total scenarios: 13
//
// #   Test Case                                             Scenario                                       Expected Result
// 1   TestPayload_JSONBodyIsCachedAndStillBindable          JSON body                                      GetPayload returns the fields and the handler can still bind the body
// 2   TestPayload_FormBodyIsCached                          URL-encoded form with a repeated key           name cached as a string, the repeated key as a 2-element []string
// 3   TestPayload_UnrecognizedContentTypeCachesRawBodyOnly  text/plain body                                GetPayload returns nil
// 4   TestPayload_OversizedBodyIsRejected                   Body over MaxBodySize                          413 with code PAYLOAD_TOO_LARGE, chain aborted
// 5   TestPayload_RouteLimitsOverrideMaxBodySize            POST to the route pattern with a higher limit  200
// 6   TestPayload_RouteLimitsOverrideMaxBodySize            PUT to the same path (limit is per method)     413
// 7   TestPayload_RouteLimitsOverrideMaxBodySize            POST to a route without its own limit          413
// 8   TestPayload_EmptyBodyPassesThrough                    GET without a body                             Not aborted, GetPayload returns nil
// 9   TestGetPayload_ReturnsNilWhenPayloadNeverRan          Payload middleware never ran                   GetPayload returns nil
// 10  TestPayload_StatusForReadFailures                     Zero MaxBodySize (default limit applies)       Not aborted
// 11  TestPayload_StatusForReadFailures                     Unknown-length body over the limit             Aborted with 413
// 12  TestPayload_StatusForReadFailures                     Body read fails with another error             Aborted with 400
// 13  TestPayload_MultipartIsLimitedButNotBuffered          multipart/form-data body                       Not buffered, body left for the handler
//
// =============================================================================

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
)

// newPayloadContext returns a test context for a POST to "/" with body and Content-Type (none if empty).
func newPayloadContext(body string, contentType string) (*httptest.ResponseRecorder, *gin.Context) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}

	return w, c
}

func TestPayload_JSONBodyIsCachedAndStillBindable(t *testing.T) {
	_, c := newPayloadContext(`{"name":"Jane"}`, "application/json")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder())(c)

	payload := middleware.GetPayload(c)
	if payload["name"] != "Jane" {
		t.Fatalf("GetPayload() = %v, want name=Jane", payload)
	}

	// The body must still be readable by a handler's own binding call.
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		t.Fatalf("ShouldBindJSON() after Payload() error = %v, want the body to still be readable", err)
	}

	if body.Name != "Jane" {
		t.Fatalf("bound Name = %q, want %q", body.Name, "Jane")
	}
}

func TestPayload_FormBodyIsCached(t *testing.T) {
	_, c := newPayloadContext("name=Jane&tag=a&tag=b", "application/x-www-form-urlencoded")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder())(c)

	payload := middleware.GetPayload(c)
	if payload["name"] != "Jane" {
		t.Fatalf(`GetPayload()["name"] = %v, want "Jane"`, payload["name"])
	}

	tags, ok := payload["tag"].([]string)
	if !ok {
		t.Fatalf(`GetPayload()["tag"] = %#v, want a []string (repeated form key)`, payload["tag"])
	}

	if len(tags) != 2 {
		t.Fatalf(`GetPayload()["tag"] = %v, want 2 elements`, tags)
	}
}

func TestPayload_UnrecognizedContentTypeCachesRawBodyOnly(t *testing.T) {
	_, c := newPayloadContext("plain text body", "text/plain")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder())(c)

	if payload := middleware.GetPayload(c); payload != nil {
		t.Fatalf("GetPayload() = %v, want nil for an unparsed content type", payload)
	}
}

func TestPayload_OversizedBodyIsRejected(t *testing.T) {
	w, c := newPayloadContext(`{"name":"this body is way too long for the configured limit"}`, "application/json")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 10}, newResponder())(c)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}

	if !c.IsAborted() {
		t.Error("chain not aborted, want an oversized body to abort it")
	}

	if !strings.Contains(w.Body.String(), `"code":"PAYLOAD_TOO_LARGE"`) {
		t.Errorf("body = %s, want code PAYLOAD_TOO_LARGE", w.Body.String())
	}
}

func TestPayload_RouteLimitsOverrideMaxBodySize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Payload(middleware.PayloadOptions{
		MaxBodySize: 10,
		RouteLimits: map[string]int64{middleware.RouteKey(http.MethodPost, "/upload/:id"): 100},
	}, newResponder()))

	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	router.POST("/upload/:id", ok)
	router.PUT("/upload/:id", ok)
	router.POST("/other", ok)

	body := strings.Repeat("x", 50) // over MaxBodySize, under the route limit
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"post to the route pattern with a higher limit", http.MethodPost, "/upload/42", http.StatusOK},
		{"put to the same path (limit is per method)", http.MethodPut, "/upload/42", http.StatusRequestEntityTooLarge},
		{"post to a route without its own limit", http.MethodPost, "/other", http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			router.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, strings.NewReader(body)))

			if w.Code != tt.want {
				t.Errorf("%s %s status = %d, want %d", tt.method, tt.path, w.Code, tt.want)
			}
		})
	}
}

func TestPayload_EmptyBodyPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, newResponder())(c)

	if c.IsAborted() {
		t.Error("chain aborted, want a request with no body to continue")
	}

	if payload := middleware.GetPayload(c); payload != nil {
		t.Fatalf("GetPayload() = %v, want nil for an empty body", payload)
	}
}

func TestGetPayload_ReturnsNilWhenPayloadNeverRan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	if payload := middleware.GetPayload(c); payload != nil {
		t.Fatalf("GetPayload() = %v, want nil when Payload() never ran", payload)
	}
}

func TestPayload_StatusForReadFailures(t *testing.T) {
	tests := []struct {
		name        string
		opts        middleware.PayloadOptions
		body        io.Reader
		wantAborted bool
		wantStatus  int
	}{
		{"zero MaxBodySize uses the default", middleware.PayloadOptions{}, strings.NewReader(`{"a":1}`), false, 0},
		{"over the limit is 413", middleware.PayloadOptions{MaxBodySize: 3}, strings.NewReader(`{"a":1}`), true, http.StatusRequestEntityTooLarge},
		{"other read error is 400", middleware.PayloadOptions{}, iotest.ErrReader(errors.New("reset")), true, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", tt.body)
			c.Request.ContentLength = -1

			middleware.Payload(tt.opts, newResponder())(c)

			if c.IsAborted() != tt.wantAborted {
				t.Fatalf("aborted = %v, want %v", c.IsAborted(), tt.wantAborted)
			}

			if tt.wantAborted && w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestPayload_MultipartIsLimitedButNotBuffered(t *testing.T) {
	_, c := newPayloadContext("--x\r\n", "multipart/form-data; boundary=x")

	middleware.Payload(middleware.PayloadOptions{}, newResponder())(c)

	if _, ok := c.Get(middleware.PayloadRawKey); ok {
		t.Fatal("multipart body was buffered, want it left unbuffered")
	}

	b, err := io.ReadAll(c.Request.Body)
	if err != nil {
		t.Fatalf("reading the request body: %v", err)
	}

	if string(b) != "--x\r\n" {
		t.Fatalf("body = %q, want it left for the handler", b)
	}
}
