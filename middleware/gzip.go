package middleware

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// GzipOptions configures response compression.
type GzipOptions struct {
	// Level is the gzip compression level (gzip.BestSpeed..gzip.BestCompression).
	// Zero or an invalid value falls back to gzip.DefaultCompression.
	Level int
}

// Gzip creates a Gin middleware that gzip-compresses responses for clients sending
// "Accept-Encoding: gzip". Responses that are already encoded, event streams, HEAD
// requests and bodiless or ranged responses (204, 206, 304) are passed through untouched.
//
// Usage:
//
//	router.Use(middleware.Gzip(middleware.GzipOptions{Level: gzip.BestSpeed}))
func Gzip(opts GzipOptions) gin.HandlerFunc {
	level := opts.Level
	if level < gzip.HuffmanOnly || level > gzip.BestCompression || level == gzip.NoCompression {
		level = gzip.DefaultCompression
	}

	pool := sync.Pool{New: func() any {
		// level is validated above, so err is always nil.
		gz, _ := gzip.NewWriterLevel(nil, level)
		return gz
	}}

	return func(c *gin.Context) {
		c.Writer.Header().Add("Vary", "Accept-Encoding")

		if c.Request.Method == http.MethodHead || !acceptsGzip(c.GetHeader("Accept-Encoding")) {
			c.Next()
			return
		}

		w := &gzipWriter{ResponseWriter: c.Writer, pool: &pool}
		c.Writer = w

		defer func() {
			if err := w.close(); err != nil {
				_ = c.Error(err)
			}

			c.Writer = w.ResponseWriter
		}()

		c.Next()
	}
}

// acceptsGzip reports whether an Accept-Encoding header allows gzip.
func acceptsGzip(header string) bool {
	for part := range strings.SplitSeq(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(coding), "gzip") {
			continue
		}

		if q, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			v, err := strconv.ParseFloat(q, 64)
			return err == nil && v > 0
		}

		return true
	}

	return false
}

// gzipWriter decides on the first body write whether to compress, so handlers
// can still set Content-Type, Content-Encoding or the status before writing.
type gzipWriter struct {
	gin.ResponseWriter
	pool    *sync.Pool
	gz      *gzip.Writer
	decided bool
	size    int
}

// Write implements io.Writer, compressing data when decide chose to.
func (w *gzipWriter) Write(data []byte) (int, error) {
	if !w.decided {
		w.decide()
	}

	if w.gz == nil {
		return w.ResponseWriter.Write(data)
	}

	n, err := w.gz.Write(data)
	w.size += n

	return n, err
}

// Size reports the bytes the handler wrote, before compression, as gin's writer does.
func (w *gzipWriter) Size() int {
	if w.gz == nil {
		return w.ResponseWriter.Size()
	}

	return w.size
}

// WriteString implements io.StringWriter through Write, so strings are compressed too.
func (w *gzipWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

// Flush pushes buffered compressed bytes to the client before flushing the connection. A flush
// commits the headers, so compression is decided first.
func (w *gzipWriter) Flush() {
	if !w.decided {
		w.decide()
	}

	if w.gz != nil {
		_ = w.gz.Flush()
	}

	w.ResponseWriter.Flush()
}

// WriteHeaderNow commits the headers. Before any body write it settles on no compression: the
// response is usually bodiless (e.g. AbortWithStatus), and a later body must match the sent headers.
func (w *gzipWriter) WriteHeaderNow() {
	w.decided = true
	w.ResponseWriter.WriteHeaderNow()
}

// decide picks compression from the headers and status the handler set, which are final once the
// body starts; already-encoded, bodiless, ranged and streamed responses pass through.
func (w *gzipWriter) decide() {
	w.decided = true

	h := w.Header()
	status := w.Status()

	if h.Get("Content-Encoding") != "" ||
		status == http.StatusNoContent || status == http.StatusNotModified || status == http.StatusPartialContent ||
		strings.HasPrefix(h.Get("Content-Type"), "text/event-stream") {
		return
	}

	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")

	w.gz = w.pool.Get().(*gzip.Writer)
	w.gz.Reset(w.ResponseWriter)
}

// close flushes the gzip footer and returns the writer to the pool, detached from the response.
func (w *gzipWriter) close() error {
	if w.gz == nil {
		return nil
	}

	err := w.gz.Close()
	w.gz.Reset(io.Discard)
	w.pool.Put(w.gz)
	w.gz = nil

	if err != nil {
		return fmt.Errorf("middleware: gzip close: %w", err)
	}

	return nil
}
