package middleware_test

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

func TestGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := strings.Repeat(`{"message":"operation_successful"}`, 50)

	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(middleware.Gzip(middleware.GzipOptions{}))
		r.GET("/json", func(c *gin.Context) { c.Data(http.StatusOK, "application/json", []byte(body)) })
		r.GET("/empty", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		r.GET("/encoded", func(c *gin.Context) {
			c.Header("Content-Encoding", "br")
			c.String(http.StatusOK, "raw")
		})
		r.GET("/sse", func(c *gin.Context) {
			c.Header("Content-Type", "text/event-stream")
			c.String(http.StatusOK, "data: hi\n\n")
		})

		return r
	}

	do := func(path, acceptEncoding string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}

		newRouter().ServeHTTP(w, req)

		return w
	}

	t.Run("compresses when the client accepts gzip", func(t *testing.T) {
		w := do("/json", "br, gzip;q=0.8")

		if got := w.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", got)
		}
		if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Errorf("Vary = %q, want Accept-Encoding", got)
		}
		if w.Body.Len() >= len(body) {
			t.Errorf("compressed size %d is not smaller than %d", w.Body.Len(), len(body))
		}

		gz, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatalf("gzip.NewReader: %v", err)
		}
		got, err := io.ReadAll(gz)
		if err != nil {
			t.Fatalf("read gzip body: %v", err)
		}
		if string(got) != body {
			t.Error("decompressed body does not match the original")
		}
	})

	t.Run("passes through without gzip or with q=0", func(t *testing.T) {
		for _, ae := range []string{"", "br", "gzip;q=0"} {
			w := do("/json", ae)
			if got := w.Header().Get("Content-Encoding"); got != "" {
				t.Errorf("Accept-Encoding %q: Content-Encoding = %q, want empty", ae, got)
			}
			if w.Body.String() != body {
				t.Errorf("Accept-Encoding %q: body was modified", ae)
			}
		}
	})

	t.Run("skips bodiless, already encoded and streaming responses", func(t *testing.T) {
		tests := map[string]string{"/empty": "", "/encoded": "br", "/sse": ""}
		for path, want := range tests {
			w := do(path, "gzip")
			if got := w.Header().Get("Content-Encoding"); got != want {
				t.Errorf("%s: Content-Encoding = %q, want %q", path, got, want)
			}
		}
	})
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
		path     string
		wantGzip bool
		wantBody string
	}{
		{"/flush", true, "streamed"},
		{"/abort", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
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

				b, _ := io.ReadAll(zr)
				body = string(b)
			}

			if body != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}
