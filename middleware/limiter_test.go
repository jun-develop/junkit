package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: limiter_test.go
// Purpose: Verify the per-client Limiter middleware: limits, client keys, whitelist, alerts and setup validation.
// Type: Unit
//
// Test functions: 8
// Total scenarios: 10
//
// #   Test Case                                      Scenario                                Expected Result
// 1   TestLimiter_AllowsWithinBurstThenRejects       Two requests with burst 1               First 200, second 429 with code TOO_MANY_REQUESTS
// 2   TestLimiter_DifferentIPsHaveIndependentLimits  First request from two IPs              Both 200
// 3   TestLimiter_IPv6ClientIsLimitedByItsSlash64    Two hosts in one /64, then another /64  200, 429 (same bucket), 200
// 4   TestLimiter_WhitelistedIPBypassesTheLimit      Five requests from a whitelisted IP     All 200
// 5   TestLimiter_UserAgentDoesNotBypass             Postman User-Agent                      Eventually 429
// 6   TestLimiter_UserAgentDoesNotBypass             Custom test User-Agent                  Eventually 429
// 7   TestLimiter_ExceedingTheLimitNotifiesOnce      Limit exceeded twice within LogTTL      429 and exactly one alert
// 8   TestLimiter_NilNotifierIsSafe                  Limit exceeded with a nil notifier      No panic, the over-limit request gets 429
// 9   TestLimiter_PanicsOnZeroRateOrBurst            RequestsPerSecond 0                     Limiter panics at setup
// 10  TestLimiter_PanicsOnZeroRateOrBurst            Burst 0                                 Limiter panics at setup
//
// =============================================================================

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/middleware"
	"github.com/jun-develop/junkit/notification"
)

// newLimiterRouter returns a router with Limiter and a GET / route answering 200.
func newLimiterRouter(opts middleware.LimiterOptions, notifier notification.Notifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Limiter(opts, zap.NewNop(), newResponder(), notifier))
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	return router
}

func TestLimiter_AllowsWithinBurstThenRejects(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

	if w := get(router, "/", "203.0.113.10:1234"); w.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d (within burst)", w.Code, http.StatusOK)
	}

	w := get(router, "/", "203.0.113.10:1234")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d (burst exhausted)", w.Code, http.StatusTooManyRequests)
	}

	if !strings.Contains(w.Body.String(), `"code":"TOO_MANY_REQUESTS"`) {
		t.Fatalf("second request body = %s, want code TOO_MANY_REQUESTS", w.Body)
	}
}

func TestLimiter_DifferentIPsHaveIndependentLimits(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

	for _, addr := range []string{"203.0.113.1:1", "203.0.113.2:1"} {
		if w := get(router, "/", addr); w.Code != http.StatusOK {
			t.Fatalf("first request from %s status = %d, want %d", addr, w.Code, http.StatusOK)
		}
	}
}

func TestLimiter_IPv6ClientIsLimitedByItsSlash64(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

	// Requests run in order and share limiter state.
	requests := []struct {
		remoteAddr string
		want       int
	}{
		{"[2001:db8:1:2::1]:1", http.StatusOK},
		// Same /64 as the first address, so it shares that bucket.
		{"[2001:db8:1:2::2]:1", http.StatusTooManyRequests},
		// Another /64 gets its own bucket.
		{"[2001:db8:1:3::1]:1", http.StatusOK},
	}

	for _, r := range requests {
		if w := get(router, "/", r.remoteAddr); w.Code != r.want {
			t.Errorf("request from %s status = %d, want %d", r.remoteAddr, w.Code, r.want)
		}
	}
}

func TestLimiter_WhitelistedIPBypassesTheLimit(t *testing.T) {
	opts := baseLimiterOptions()
	opts.IPWhitelist = []string{"203.0.113.10"}
	router := newLimiterRouter(opts, newFakeNotifier())

	for i := range 5 {
		if w := get(router, "/", "203.0.113.10:1234"); w.Code != http.StatusOK {
			t.Fatalf("request %d from a whitelisted IP status = %d, want %d", i, w.Code, http.StatusOK)
		}
	}
}

// The User-Agent is set by the client, so no value of it may skip the limiter.
func TestLimiter_UserAgentDoesNotBypass(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
	}{
		{"postman user agent", "PostmanRuntime/7.36.0"},
		{"custom test user agent", "GoHttpTest/1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

			limited := false
			for i := 0; i < 50 && !limited; i++ {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.RemoteAddr = "203.0.113.10:1234"
				r.Header.Set("User-Agent", tt.userAgent)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				limited = w.Code == http.StatusTooManyRequests
			}

			if !limited {
				t.Fatalf("User-Agent %q was never rate limited", tt.userAgent)
			}
		})
	}
}

func TestLimiter_ExceedingTheLimitNotifiesOnce(t *testing.T) {
	notifier := newFakeNotifier()
	opts := baseLimiterOptions()
	opts.LogTTL = time.Hour
	router := newLimiterRouter(opts, notifier)

	// Burst 1: the first request passes, the second exceeds the limit and alerts, the third
	// exceeds it again within LogTTL and must not alert again.
	get(router, "/", "203.0.113.20:1234")
	get(router, "/", "203.0.113.20:1234")
	w := get(router, "/", "203.0.113.20:1234")

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third request status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	select {
	case <-notifier.messages:
	case <-time.After(time.Second):
		t.Fatal("no alert was sent after exceeding the rate limit")
	}

	select {
	case msg := <-notifier.messages:
		t.Fatalf("got a second alert %q within LogTTL, want the throttle to suppress it", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestLimiter_NilNotifierIsSafe(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), nil)

	var w *httptest.ResponseRecorder
	for range 3 {
		w = get(router, "/", "203.0.113.30:1234")
	}

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit request status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
}

func TestLimiter_PanicsOnZeroRateOrBurst(t *testing.T) {
	noRate := baseLimiterOptions()
	noRate.RequestsPerSecond = 0
	noBurst := baseLimiterOptions()
	noBurst.Burst = 0

	tests := []struct {
		name string
		opts middleware.LimiterOptions
	}{
		{"requests per second is zero", noRate},
		{"burst is zero", noBurst},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Limiter(%+v) did not panic", tt.opts)
				}
			}()

			middleware.Limiter(tt.opts, zap.NewNop(), newResponder(), nil)
		})
	}
}
