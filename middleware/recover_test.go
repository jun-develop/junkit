package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: recover_test.go
// Purpose: Verify the Recover middleware turns panics into 500 responses and error reports.
// Type: Unit
//
// Test functions: 7
// Total scenarios: 7
//
// #  Test Case                                    Scenario                                          Expected Result
// 1  TestRecover_NoPanicPassesThrough             Handler returns 200 normally                      200
// 2  TestRecover_PanicIsCaughtAndReported         Handler panics with a string                      500 and one error report with status 500 and the panic message
// 3  TestRecover_NilNotifierIsSafe                Handler panics, notifier is nil                   500, no crash
// 4  TestRecover_BreakpointSignalIsRePanicked     Handler panics with tracer.BreakpointSignal       Signal re-panicked unchanged, no error report
// 5  TestRecover_DebugBypassRoutesThroughTracer   Panic on a debug request with the secret header   No response written, no error report
// 6  TestRecover_DebugWithoutSecretNeverBypasses  Panic on a debug query without the secret header  500 and one error report (normal path)
// 7  TestRecover_ReportsThePanicSite              Handler panics on a known line                    Report location is recover_test.go at the panic line
//
// =============================================================================

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/notification"
	"github.com/jun-develop/junkit/tracer"
)

// newRecoverRouter returns a router with Recover and a GET / route served by handler.
func newRecoverRouter(notifier notification.Notifier, opts middleware.RecoverOptions, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Recover(opts, zap.NewNop(), notifier, newResponder()))
	router.GET("/", handler)

	return router
}

func TestRecover_NoPanicPassesThrough(t *testing.T) {
	notifier := newFakeNotifier()
	router := newRecoverRouter(notifier, middleware.RecoverOptions{}, func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRecover_PanicIsCaughtAndReported(t *testing.T) {
	notifier := newFakeNotifier()
	router := newRecoverRouter(notifier, middleware.RecoverOptions{}, func(c *gin.Context) {
		panic("something broke")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}

	notifications := notifier.errorReports()
	if len(notifications) != 1 {
		t.Fatalf("got %d error notifications, want 1", len(notifications))
	}

	if notifications[0].StatusCode != http.StatusInternalServerError {
		t.Fatalf("notified StatusCode = %d, want %d", notifications[0].StatusCode, http.StatusInternalServerError)
	}

	if notifications[0].Error != "something broke" {
		t.Fatalf(`notified Error = %v, want "something broke"`, notifications[0].Error)
	}
}

func TestRecover_NilNotifierIsSafe(t *testing.T) {
	router := newRecoverRouter(nil, middleware.RecoverOptions{}, func(c *gin.Context) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d even with a nil notifier", w.Code, http.StatusInternalServerError)
	}
}

func TestRecover_BreakpointSignalIsRePanicked(t *testing.T) {
	notifier := newFakeNotifier()
	router := newRecoverRouter(notifier, middleware.RecoverOptions{}, func(c *gin.Context) {
		panic(tracer.BreakpointSignal{})
	})

	defer func() {
		r := recover()
		if _, ok := r.(tracer.BreakpointSignal); !ok {
			t.Fatalf("recover() = %v (%T), want it to re-panic a tracer.BreakpointSignal untouched", r, r)
		}

		if len(notifier.errorReports()) != 0 {
			t.Error("got an error notification, want none: a BreakpointSignal is not a real error")
		}
	}()

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRecover_DebugBypassRoutesThroughTracer(t *testing.T) {
	notifier := newFakeNotifier()
	opts := middleware.RecoverOptions{Debug: debugOpts}
	router := newRecoverRouter(notifier, opts, func(c *gin.Context) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, newDebugRequest("/?t_debug=1234"))

	// Debug mode delegates to tracer.Error without writing a response.
	if w.Code != http.StatusOK && w.Code != 0 {
		t.Fatalf("status = %d, want no response written on the debug bypass path", w.Code)
	}

	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty on the debug bypass path", w.Body)
	}

	if len(notifier.errorReports()) != 0 {
		t.Error("got an error notification, want none on the debug bypass path")
	}
}

func TestRecover_DebugWithoutSecretNeverBypasses(t *testing.T) {
	notifier := newFakeNotifier()
	opts := middleware.RecoverOptions{Debug: debugOpts}
	router := newRecoverRouter(notifier, opts, func(c *gin.Context) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/?t_debug=1234", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d: a debug query without the secret must never bypass the normal error path",
			w.Code, http.StatusInternalServerError)
	}

	if len(notifier.errorReports()) != 1 {
		t.Fatalf("got %d error notifications, want 1 (the normal path should still notify)", len(notifier.errorReports()))
	}
}

func TestRecover_ReportsThePanicSite(t *testing.T) {
	notifier := newFakeNotifier()
	var panicLine int
	router := newRecoverRouter(notifier, middleware.RecoverOptions{}, func(c *gin.Context) {
		_, _, panicLine, _ = runtime.Caller(0)
		panic("boom") // must stay on the line right after runtime.Caller
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	reports := notifier.errorReports()
	if len(reports) != 1 {
		t.Fatalf("got %d error notifications, want 1", len(reports))
	}

	if !strings.HasSuffix(reports[0].Location, "recover_test.go") {
		t.Errorf("location = %v, want recover_test.go (the panic site, not Recover's handler)", reports[0].Location)
	}

	if reports[0].Line != panicLine+1 {
		t.Errorf("line = %d, want %d (the panic line)", reports[0].Line, panicLine+1)
	}
}
