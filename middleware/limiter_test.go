package middleware_test

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

func newLimiterRouter(opts middleware.LimiterOptions, notifier notification.Notifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Limiter(opts, zap.NewNop(), newResponder(), notifier))
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	return router
}

func baseLimiterOptions() middleware.LimiterOptions {
	return middleware.LimiterOptions{
		RequestsPerSecond: 1,
		Burst:             1,
		CleanupInterval:   time.Minute,
		ClientTTL:         time.Minute,
		LogTTL:            time.Minute,
	}
}

func TestLimiter_AllowsWithinBurstThenRejects(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

	req := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "203.0.113.10:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)

		return w
	}

	if w := req(); w.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d (within burst)", w.Code, http.StatusOK)
	}

	if w := req(); w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), `"code":"TOO_MANY_REQUESTS"`) {
		t.Fatalf("second request = %d %s, want %d with code TOO_MANY_REQUESTS (burst exhausted)", w.Code, w.Body, http.StatusTooManyRequests)
	}
}

func TestLimiter_DifferentIPsHaveIndependentLimits(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

	for _, ip := range []string{"203.0.113.1:1", "203.0.113.2:1"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = ip
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("first request from %s status = %d, want %d", ip, w.Code, http.StatusOK)
		}
	}
}

func TestLimiter_IPv6ClientIsLimitedByItsSlash64(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

	tests := []struct {
		remoteAddr string
		want       int
	}{
		{"[2001:db8:1:2::1]:1", http.StatusOK},
		{"[2001:db8:1:2::2]:1", http.StatusTooManyRequests}, // same /64: same bucket
		{"[2001:db8:1:3::1]:1", http.StatusOK},              // another /64
	}

	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tt.remoteAddr
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("request from %s status = %d, want %d", tt.remoteAddr, w.Code, tt.want)
		}
	}
}

func TestLimiter_WhitelistedIPBypassesTheLimit(t *testing.T) {
	opts := baseLimiterOptions()
	opts.IPWhitelist = []string{"203.0.113.10"}
	router := newLimiterRouter(opts, newFakeNotifier())

	for i := 0; i < 5; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "203.0.113.10:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d from a whitelisted IP status = %d, want %d", i, w.Code, http.StatusOK)
		}
	}
}

// The User-Agent is set by the client, so no value of it may skip the limiter.
func TestLimiter_UserAgentDoesNotBypass(t *testing.T) {
	tests := []string{"PostmanRuntime/7.36.0", "GoHttpTest/1.0"}

	for _, ua := range tests {
		t.Run(ua, func(t *testing.T) {
			router := newLimiterRouter(baseLimiterOptions(), newFakeNotifier())

			limited := false
			for i := 0; i < 50 && !limited; i++ {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.RemoteAddr = "203.0.113.10:1234"
				r.Header.Set("User-Agent", ua)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				limited = w.Code == http.StatusTooManyRequests
			}

			if !limited {
				t.Fatalf("User-Agent %q was never rate limited", ua)
			}
		})
	}
}

func TestLimiter_ExceedingTheLimitNotifiesOnce(t *testing.T) {
	notifier := newFakeNotifier()
	opts := baseLimiterOptions()
	opts.LogTTL = time.Hour // keep the log/alert throttle open for the whole test
	router := newLimiterRouter(opts, notifier)

	send := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "203.0.113.20:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)

		return w
	}

	send()      // consumes the single burst token
	send()      // exceeds the limit -> should notify
	w := send() // exceeds again -> should NOT notify again within LogTTL

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	select {
	case <-notifier.messages:
	case <-time.After(time.Second):
		t.Fatal("expected an alert message to be sent after exceeding the rate limit")
	}

	select {
	case msg := <-notifier.messages:
		t.Fatalf("got a second alert message %q within LogTTL, want the throttle to suppress it", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestLimiter_NilNotifierIsSafe(t *testing.T) {
	router := newLimiterRouter(baseLimiterOptions(), nil)

	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "203.0.113.30:1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
	}
	// The real assertion is that none of this panicked with a nil notifier.
}

func TestLimiter_PanicsOnZeroRateOrBurst(t *testing.T) {
	noRate, noBurst := baseLimiterOptions(), baseLimiterOptions()
	noRate.RequestsPerSecond, noBurst.Burst = 0, 0

	for _, opts := range []middleware.LimiterOptions{noRate, noBurst} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Limiter(%+v) did not panic", opts)
				}
			}()
			middleware.Limiter(opts, zap.NewNop(), newResponder(), nil)
		}()
	}
}
