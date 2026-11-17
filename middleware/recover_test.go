package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/notification"
	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/tracer"
)

func newResponder() response.Responder {
	return response.NewResponder(i18n.NewTranslator())
}

// newRecoverRouter sets up a Gin router with Recover middleware for testing.
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
			t.Error("a BreakpointSignal is not a real error and should not trigger a notification")
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

	if len(notifier.errorReports()) != 0 {
		t.Error("the debug bypass path should not send an error notification")
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
		t.Fatalf("status = %d, want %d — a debug query without the secret must never bypass the normal error path",
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

	report := notifier.errorReports()[0]
	if !strings.HasSuffix(report.Location, "recover_test.go") || report.Line != panicLine+1 {
		t.Fatalf("location = %v:%v, want recover_test.go:%d (the panic site, not Recover's handler)", report.Location, report.Line, panicLine+1)
	}
}
