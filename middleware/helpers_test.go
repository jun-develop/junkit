package middleware_test

// =============================================================================
// TEST HELPERS (no test scenarios)
// =============================================================================
//
// File: helpers_test.go
// Purpose: Fixtures shared by several middleware test files: limiter and debug-dashboard
//          options, a responder, request helpers and a fake notifier that records alerts
//          and error reports.
// Used by: access_log_test.go, banner_test.go, limiter_test.go, payload_test.go,
//          recover_test.go, route_patterns_test.go, tracer_debug_test.go
//
// =============================================================================

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/notification"
	"github.com/jun-develop/junkit/response"
)

// baseLimiterOptions allows 1 request per second with a burst of 1.
func baseLimiterOptions() middleware.LimiterOptions {
	return middleware.LimiterOptions{
		RequestsPerSecond: 1,
		Burst:             1,
		CleanupInterval:   time.Minute,
		ClientTTL:         time.Minute,
		LogTTL:            time.Minute,
	}
}

// debugOpts activates the debug dashboard with ?t_debug=1234 plus the X-Internal-Secret header.
var debugOpts = middleware.TracerDebugOptions{
	QueryKey:     "t_debug",
	QueryValue:   "1234",
	SecretHeader: "X-Internal-Secret",
	Secret:       "s3cret",
}

// newDebugRequest builds a GET for target carrying debugOpts' secret header.
func newDebugRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set(debugOpts.SecretHeader, debugOpts.Secret)

	return req
}

// newResponder returns a real Responder with the default translator.
func newResponder() response.Responder {
	return response.NewResponder(i18n.NewTranslator())
}

// get serves a GET for path from remoteAddr and returns the response.
func get(router *gin.Engine, path, remoteAddr string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)

	return w
}

// fakeNotifier is a notification.Notifier that records alerts on a buffered channel (read them
// with a timeout) and error reports.
type fakeNotifier struct {
	messages chan string

	mu      sync.Mutex
	reports []notification.ErrorReport
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{messages: make(chan string, 8)}
}

func (f *fakeNotifier) Send(_ context.Context, _ notification.Level, msg string) {
	f.messages <- msg
}

func (f *fakeNotifier) ReportError(_ context.Context, _ *http.Request, report notification.ErrorReport) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, report)
}

// errorReports returns a copy of the error reports received so far.
func (f *fakeNotifier) errorReports() []notification.ErrorReport {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]notification.ErrorReport(nil), f.reports...)
}
