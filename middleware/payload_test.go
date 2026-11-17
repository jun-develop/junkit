package middleware_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/response"
)

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
	responder := response.NewResponder(i18n.NewTranslator())
	w, c := newPayloadContext(`{"name":"Jane"}`, "application/json")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, responder)(c)

	payload := middleware.GetPayload(c)
	if payload["name"] != "Jane" {
		t.Fatalf("GetPayload() = %v, want name=Jane", payload)
	}

	// The body must still be readable by a handler's own binding call.
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		t.Fatalf("ShouldBindJSON after Payload() error = %v, want the body to still be readable", err)
	}

	if body.Name != "Jane" {
		t.Fatalf("bound Name = %q, want %q", body.Name, "Jane")
	}

	_ = w
}

func TestPayload_FormBodyIsCached(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	_, c := newPayloadContext("name=Jane&tag=a&tag=b", "application/x-www-form-urlencoded")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, responder)(c)

	payload := middleware.GetPayload(c)
	if payload["name"] != "Jane" {
		t.Fatalf(`GetPayload()["name"] = %v, want "Jane"`, payload["name"])
	}

	if tags, ok := payload["tag"].([]string); !ok || len(tags) != 2 {
		t.Fatalf(`GetPayload()["tag"] = %#v, want a 2-element []string (repeated form key)`, payload["tag"])
	}
}

func TestPayload_UnrecognizedContentTypeCachesRawBodyOnly(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	_, c := newPayloadContext("plain text body", "text/plain")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, responder)(c)

	if payload := middleware.GetPayload(c); payload != nil {
		t.Fatalf("GetPayload() = %v, want nil for an unparsed content type", payload)
	}
}

func TestPayload_OversizedBodyIsRejected(t *testing.T) {
	responder := response.NewResponder(i18n.NewTranslator())
	w, c := newPayloadContext(`{"name":"this body is way too long for the configured limit"}`, "application/json")

	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 10}, responder)(c)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}

	if !c.IsAborted() {
		t.Error("an oversized body should abort the chain")
	}

	if !strings.Contains(w.Body.String(), `"code":"PAYLOAD_TOO_LARGE"`) {
		t.Errorf("body = %s, want code PAYLOAD_TOO_LARGE", w.Body.String())
	}
}

func TestPayload_RouteLimitsOverrideMaxBodySize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	responder := response.NewResponder(i18n.NewTranslator())
	router := gin.New()
	router.Use(middleware.Payload(middleware.PayloadOptions{
		MaxBodySize: 10,
		RouteLimits: map[string]int64{middleware.RouteKey(http.MethodPost, "/upload/:id"): 100},
	}, responder))

	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	router.POST("/upload/:id", ok)
	router.PUT("/upload/:id", ok)
	router.POST("/other", ok)

	body := strings.Repeat("x", 50) // over MaxBodySize, under the route limit
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/upload/42", http.StatusOK},                   // matched by pattern, not raw path
		{http.MethodPut, "/upload/42", http.StatusRequestEntityTooLarge}, // the limit is per method
		{http.MethodPost, "/other", http.StatusRequestEntityTooLarge},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(body)))
		if w.Code != tc.want {
			t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, w.Code, tc.want)
		}
	}
}

func TestPayload_EmptyBodyPassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	responder := response.NewResponder(i18n.NewTranslator())
	middleware.Payload(middleware.PayloadOptions{MaxBodySize: 1 << 20}, responder)(c)

	if c.IsAborted() {
		t.Error("a request with no body should not be aborted")
	}

	if payload := middleware.GetPayload(c); payload != nil {
		t.Fatalf("GetPayload() = %v, want nil for an empty body", payload)
	}
}

func TestGetPayload_NothingCached(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	if payload := middleware.GetPayload(c); payload != nil {
		t.Fatalf("GetPayload() = %v, want nil when Payload() never ran", payload)
	}
}

func TestPayload_StatusForReadFailures(t *testing.T) {
	tests := []struct {
		name    string
		opts    middleware.PayloadOptions
		body    io.Reader
		want    int
		aborted bool
	}{
		{"zero MaxBodySize uses the default", middleware.PayloadOptions{}, strings.NewReader(`{"a":1}`), http.StatusOK, false},
		{"over the limit is 413", middleware.PayloadOptions{MaxBodySize: 3}, strings.NewReader(`{"a":1}`), http.StatusRequestEntityTooLarge, true},
		{"other read error is 400", middleware.PayloadOptions{}, iotest.ErrReader(errors.New("reset")), http.StatusBadRequest, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/", tt.body)
			c.Request.ContentLength = -1

			middleware.Payload(tt.opts, response.NewResponder(i18n.NewTranslator()))(c)

			if c.IsAborted() != tt.aborted || (tt.aborted && w.Code != tt.want) {
				t.Fatalf("aborted = %v, status = %d, want aborted %v and %d", c.IsAborted(), w.Code, tt.aborted, tt.want)
			}
		})
	}
}

func TestPayload_MultipartIsLimitedButNotBuffered(t *testing.T) {
	_, c := newPayloadContext("--x\r\n", "multipart/form-data; boundary=x")

	middleware.Payload(middleware.PayloadOptions{}, response.NewResponder(i18n.NewTranslator()))(c)

	if _, ok := c.Get(middleware.PayloadRawKey); ok {
		t.Fatal("a multipart body was buffered")
	}

	if b, _ := io.ReadAll(c.Request.Body); string(b) != "--x\r\n" {
		t.Fatalf("body = %q, want it left for the handler", b)
	}
}
