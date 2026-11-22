package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: gzip_test.go
// Purpose: Verify the Gzip middleware compresses only eligible responses and keeps headers consistent with the body.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 12
//
// #   Test Case                                   Scenario                                     Expected Result
// 1   TestGzip_CompressesWhenClientAcceptsGzip    Accept-Encoding "br, gzip;q=0.8"             Content-Encoding gzip, Vary set, smaller body that decompresses to the original
// 2   TestGzip_PassesThroughWhenGzipNotAccepted   No Accept-Encoding                           No Content-Encoding, body unchanged
// 3   TestGzip_PassesThroughWhenGzipNotAccepted   Accept-Encoding "br"                         No Content-Encoding, body unchanged
// 4   TestGzip_PassesThroughWhenGzipNotAccepted   Accept-Encoding "gzip;q=0"                   No Content-Encoding, body unchanged
// 5   TestGzip_SkipsIneligibleResponses           204 response without a body                  No Content-Encoding
// 6   TestGzip_SkipsIneligibleResponses           Response already encoded with br             Content-Encoding stays br
// 7   TestGzip_SkipsIneligibleResponses           text/event-stream response                   No Content-Encoding
// 8   TestGzip_HeadersCommittedEarlyMatchTheBody  Handler flushes before writing               Content-Encoding gzip and the body is valid gzip
// 9   TestGzip_HeadersCommittedEarlyMatchTheBody  Handler aborts with 401                      No Content-Encoding and an empty body
// 10  TestGzip_WriteStringAndSizeUseRawBytes      WriteString, client accepts gzip             Body is gzip of the string; Size() is the uncompressed length
// 11  TestGzip_WriteStringAndSizeUseRawBytes      WriteString, gzip not accepted               Body is the plain string; Size() is its length
// 12  TestGzip_WriteStringAndSizeUseRawBytes      WriteString, gzip accepted but event stream  Body is the plain string; Size() is its length
//
// =============================================================================

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/middleware"
)

var gzipTestBody = strings.Repeat(`{"message":"operation_successful"}`, 50)

// serveGzip serves a GET for path through Gzip with the given Accept-Encoding (none if empty).
// Routes: /json (gzipTestBody), /empty (204), /encoded (already br), /sse (event stream).
func serveGzip(path, acceptEncoding string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Gzip(middleware.GzipOptions{}))
	r.GET("/json", func(c *gin.Context) { c.Data(http.StatusOK, "application/json", []byte(gzipTestBody)) })
	r.GET("/empty", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/encoded", func(c *gin.Context) {
		c.Header("Content-Encoding", "br")
		c.String(http.StatusOK, "raw")
	})
	r.GET("/sse", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, "data: hi\n\n")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}

	r.ServeHTTP(w, req)

	return w
}

func TestGzip_CompressesWhenClientAcceptsGzip(t *testing.T) {
	w := serveGzip("/json", "br, gzip;q=0.8")

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", got)
	}

	if w.Body.Len() >= len(gzipTestBody) {
		t.Errorf("compressed size %d is not smaller than %d", w.Body.Len(), len(gzipTestBody))
	}

	gz, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}

	got, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("reading the gzip body: %v", err)
	}

	if string(got) != gzipTestBody {
		t.Error("decompressed body does not match the original")
	}
}

func TestGzip_PassesThroughWhenGzipNotAccepted(t *testing.T) {
	tests := []struct {
		name           string
		acceptEncoding string
	}{
		{"no accept-encoding", ""},
		{"only br accepted", "br"},
		{"gzip refused with q=0", "gzip;q=0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveGzip("/json", tt.acceptEncoding)

			if got := w.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("Content-Encoding = %q, want empty", got)
			}

			if w.Body.String() != gzipTestBody {
				t.Error("body was modified, want it unchanged")
			}
		})
	}
}

func TestGzip_SkipsIneligibleResponses(t *testing.T) {
	tests := []struct {
		name                string
		path                string
		wantContentEncoding string
	}{
		{"204 response without a body", "/empty", ""},
		{"response already encoded with br", "/encoded", "br"},
		{"text/event-stream response", "/sse", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveGzip(tt.path, "gzip")

			if got := w.Header().Get("Content-Encoding"); got != tt.wantContentEncoding {
				t.Errorf("Content-Encoding = %q, want %q", got, tt.wantContentEncoding)
			}
		})
	}
}

func TestGzip_HeadersCommittedEarlyMatchTheBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Gzip(middleware.GzipOptions{}))
	r.GET("/flush", func(c *gin.Context) {
		c.Writer.Flush()
		c.String(http.StatusOK, "streamed")
	})
	r.GET("/abort", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})

	tests := []struct {
		name     string
		path     string
		wantGzip bool
		wantBody string
	}{
		{"handler flushes before writing", "/flush", true, "streamed"},
		{"handler aborts with 401", "/abort", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Accept-Encoding", "gzip")

			r.ServeHTTP(w, req)

			if gotGzip := w.Header().Get("Content-Encoding") == "gzip"; gotGzip != tt.wantGzip {
				t.Fatalf("Content-Encoding gzip = %v, want %v", gotGzip, tt.wantGzip)
			}

			body := w.Body.String()
			if tt.wantGzip {
				zr, err := gzip.NewReader(w.Body)
				if err != nil {
					t.Fatalf("gzip.NewReader() error = %v (body not compressed though headers say so)", err)
				}

				b, err := io.ReadAll(zr)
				if err != nil {
					t.Fatalf("reading the gzip body: %v", err)
				}

				body = string(b)
			}

			if body != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

func TestGzip_WriteStringAndSizeUseRawBytes(t *testing.T) {
	tests := []struct {
		name           string
		acceptEncoding string
		contentType    string
		wantGzip       bool
	}{
		{"client accepts gzip", "gzip", "application/json", true},
		{"gzip not accepted", "", "application/json", false},
		{"gzip accepted but event stream", "gzip", "text/event-stream", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(middleware.Gzip(middleware.GzipOptions{}))

			var size int
			router.GET("/", func(c *gin.Context) {
				c.Header("Content-Type", tt.contentType)
				c.Status(http.StatusOK)
				if _, err := c.Writer.WriteString(gzipTestBody); err != nil {
					t.Errorf("WriteString() error = %v", err)
				}

				size = c.Writer.Size()
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if size != len(gzipTestBody) {
				t.Errorf("Size() = %d, want %d (bytes before compression)", size, len(gzipTestBody))
			}

			body := w.Body.String()
			if tt.wantGzip {
				gz, err := gzip.NewReader(w.Body)
				if err != nil {
					t.Fatalf("gzip.NewReader() error = %v (Content-Encoding %q)", err, w.Header().Get("Content-Encoding"))
				}

				raw, err := io.ReadAll(gz)
				if err != nil {
					t.Fatalf("reading the gzip body: %v", err)
				}

				body = string(raw)
			}

			if body != gzipTestBody {
				t.Errorf("body = %.40q..., want gzipTestBody", body)
			}
		})
	}
}
