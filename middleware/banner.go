package middleware

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/cache"
	"github.com/jun-develop/junkit/notification"
	"github.com/jun-develop/junkit/response"
	"github.com/jun-develop/junkit/security"
)

// The default trap patterns hold only signs no real client sends, since one match bans at once;
// anything vaguer is left to strikes. Patterns see their input lowercased. Each starts with a
// literal, which regexp finds with a plain substring search before running the matcher, so a clean
// request costs far less than one large alternation would. Extend a list with append; an empty
// non-nil list turns that check off.
var (
	// DefaultTrapPaths match the URL path of a request that matched no route, at any depth.
	DefaultTrapPaths = patterns(
		// Dotfiles and secrets: /.env.example, /api/.git/config, /.aws/credentials.
		`/\.(env|git|svn|hg|bzr|aws|ssh|docker|kube|npmrc|pypirc|netrc|bash_history|zsh_history|mysql_history|`+
			`htaccess|htpasswd|ds_store|idea|vscode|terraform|circleci|travis|gitlab-ci|sentryclirc|vault-token)`,
		`\.env(\.|/|$)`, // /production.env, /app.env.bak
		`/id_(rsa|dsa|ecdsa|ed25519)(\.pub)?$`,
		// File types a Go API never serves: PHP/ASP/Java/CGI pages, dumps and editor backups.
		`\.(php\d?|phtml|phar|aspx?|ashx|asmx|axd|jspx?|do|action|cgi|pl|sql|sqlite3?|mdb|bak|old|orig|swp)(/|$)`,
		// Admin panels, CMSs and appliances scanners look for.
		`/(wp-admin|wp-content|wp-includes|wp-config|wordpress|xmlrpc|phpmyadmin|pma|myadmin|adminer|`+
			`cgi-bin|fcgi-bin|vendor/phpunit|phpunit|server-status|server-info|actuator|jolokia|solr|jenkins|hudson|`+
			`manager/html|invoker|axis2|wls-wsat|_ignition|telescope|_profiler|elfinder|fckeditor|kcfinder|`+
			`boaform|hnap1|gponform|cpanel|owa|ecp|autodiscover|remote/login|\+cscoe\+|dana-na|global-protect|`+
			`sslvpn|geoserver|nacos|druid|xxl-job|api/jsonws)(/|$)`,
		// Path traversal and system files.
		`\.\./`, `\.\.\\`, `/etc/(passwd|shadow|hosts)`, `/proc/self`, `win\.ini`, `boot\.ini`, `web-inf`, `meta-inf`,
	)

	// DefaultAttackPatterns match injection payloads in the URL-decoded query, and in the path of a
	// request that matched no route.
	DefaultAttackPatterns = patterns(
		`\$\{(jndi|env|sys|java|lower|upper|::-)`, // Log4Shell
		`<script`, `javascript:`, `onerror\s*=`, `onload\s*=`,
		`union(\s|\+|/\*.*\*/)+(all(\s|\+)+)?select`, `information_schema`, `sleep\(\s*\d`, `benchmark\(`,
		`waitfor\s+delay`, `pg_sleep`,
		`;\s*`+shellCommand, `\|\s*`+shellCommand, `&&\s*`+shellCommand, "`\\s*"+shellCommand, `\$\(\s*`+shellCommand,
		`php://`, `file://`, `expect://`, `gopher://`, `dict://`, `phar://`, `data://text`,
		`169\.254\.169\.254`, `metadata\.google\.internal`,
		`\x00`,
	)

	// DefaultScannerAgents match the User-Agent of vulnerability scanners and fuzzers.
	DefaultScannerAgents = patterns(
		`sqlmap`, `nikto`, `nuclei`, `projectdiscovery`, `nmap`, `masscan`, `zgrab`, `zmap`, `acunetix`,
		`netsparker`, `wpscan`, `dirbuster`, `gobuster`, `feroxbuster`, `ffuf`, `wfuzz`, `dirsearch`, `openvas`,
		`nessus`, `qualys`, `burp`, `w3af`, `arachni`, `skipfish`, `whatweb`, `jaeles`, `commix`, `hydra`,
		`fuzz faster`, `l9explore`, `l9tcpid`, `censysinspect`,
	)
)

// shellCommand is a command an injected shell payload typically runs.
const shellCommand = `(wget|curl|bash|sh|nc|chmod|python|perl)\s`

// patterns compiles exprs.
func patterns(exprs ...string) []*regexp.Regexp {
	res := make([]*regexp.Regexp, len(exprs))
	for i, e := range exprs {
		res[i] = regexp.MustCompile(e)
	}

	return res
}

// matchAny reports whether one of res matches s.
func matchAny(res []*regexp.Regexp, s string) bool {
	return slices.ContainsFunc(res, func(re *regexp.Regexp) bool { return re.MatchString(s) })
}

// DefaultStrikeStatuses are the response codes a scanner probing an API mostly receives.
var DefaultStrikeStatuses = []int{
	http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
	http.StatusMethodNotAllowed, http.StatusTooManyRequests,
}

// BannerOptions configures Banner. Clients are keyed by security.ClientKey (an IPv6 client is its /64).
type BannerOptions struct {
	// Cache holds strikes and bans. Use Redis to share them across instances; a memory cache
	// works per instance. A no-op cache disables banning.
	Cache cache.Cache[int64]

	// TrapPaths ban a client on the first request that matched no route and whose lowercased
	// path one of them matches. Defaults to DefaultTrapPaths when nil.
	TrapPaths []*regexp.Regexp

	// AttackPatterns ban a client on the first request whose lowercased, URL-decoded query (and
	// path, when it matched no route) one of them matches. Defaults to DefaultAttackPatterns when
	// nil. Request bodies are not checked.
	AttackPatterns []*regexp.Regexp

	// ScannerAgents ban a client on the first request whose lowercased User-Agent one of them
	// matches. Defaults to DefaultScannerAgents when nil.
	ScannerAgents []*regexp.Regexp

	// StrikeStatuses are the response codes that count as a strike. Defaults to
	// DefaultStrikeStatuses when nil.
	StrikeStatuses []int

	// MaxStrikes within StrikeWindow bans the client.
	MaxStrikes int64

	// StrikeWindow is the fixed window strikes are counted in.
	StrikeWindow time.Duration

	// BanDurations are applied in turn to a client banned again; the last one repeats. A client's
	// ban count is forgotten twice the longest duration after its first ban.
	BanDurations []time.Duration

	// IPWhitelist lists client IPs that are never banned.
	IPWhitelist []string
}

// Banner creates a Gin middleware that bans clients probing the API: a request to a trap path
// bans at once, and MaxStrikes responses with a strike status within StrikeWindow ban too.
// A banned client gets 403 with Retry-After until the ban ends. Place it before Limiter, so
// banned requests skip it and its 429s count as strikes.
//
// Usage:
//
//	router.Use(middleware.Banner(middleware.BannerOptions{
//	    Cache: cache.New[int64](cacheOpts), MaxStrikes: 30, StrikeWindow: time.Minute,
//	    BanDurations: []time.Duration{15 * time.Minute, time.Hour, 24 * time.Hour},
//	}, zapLogger, responder, notifier), middleware.Limiter(...))
func Banner(opts BannerOptions, logger *zap.Logger, responder response.Responder, notifier notification.Notifier) gin.HandlerFunc {
	if opts.Cache == nil || opts.MaxStrikes <= 0 || opts.StrikeWindow <= 0 || len(opts.BanDurations) == 0 {
		panic("middleware: Banner requires Cache, MaxStrikes > 0, StrikeWindow > 0 and BanDurations")
	}

	if opts.TrapPaths == nil {
		opts.TrapPaths = DefaultTrapPaths
	}

	if opts.AttackPatterns == nil {
		opts.AttackPatterns = DefaultAttackPatterns
	}

	if opts.ScannerAgents == nil {
		opts.ScannerAgents = DefaultScannerAgents
	}

	if opts.StrikeStatuses == nil {
		opts.StrikeStatuses = DefaultStrikeStatuses
	}

	whitelist := make(map[string]struct{}, len(opts.IPWhitelist))
	for _, ip := range opts.IPWhitelist {
		whitelist[ip] = struct{}{}
	}

	b := &banner{opts: opts, logger: logger, notifier: notifier, historyTTL: 2 * slices.Max(opts.BanDurations)}

	return func(ctx *gin.Context) {
		ip := ctx.ClientIP()
		if _, ok := whitelist[ip]; ok {
			ctx.Next()
			return
		}

		key := security.ClientKey(ip)
		if until, ok := opts.Cache.Get(ctx, banKey(key)); ok {
			deny(ctx, responder, until)
			return
		}

		if reason := trapped(ctx, opts); reason != "" {
			deny(ctx, responder, b.ban(ctx, ip, key, reason))
			return
		}

		ctx.Next()

		if !slices.Contains(opts.StrikeStatuses, ctx.Writer.Status()) {
			return
		}

		n, err := opts.Cache.Incr(ctx, strikeKey(key), opts.StrikeWindow)
		if err != nil {
			logger.Error("middleware: banner strike count failed", zap.String("ip", ip), zap.Error(err))
			return
		}

		// == bans once per window even when concurrent strikes pass the limit together.
		if n == opts.MaxStrikes {
			_ = opts.Cache.Delete(ctx, strikeKey(key))
			b.ban(ctx, ip, key, strconv.FormatInt(n, 10)+" strikes")
		}
	}
}

// trapped returns why the request bans its client at once, or "" when it does not. A path that
// matched a route is not checked: no real route is a trap, and the patterns cost far more on a
// path than a route lookup does.
func trapped(ctx *gin.Context, opts BannerOptions) string {
	r := ctx.Request
	if matchAny(opts.ScannerAgents, strings.ToLower(r.UserAgent())) {
		return "scanner user agent"
	}

	var target string
	if ctx.FullPath() == "" {
		target = strings.ToLower(r.URL.Path)
		if matchAny(opts.TrapPaths, target) {
			return "trap path"
		}
	}

	if r.URL.RawQuery != "" {
		query, err := url.QueryUnescape(r.URL.RawQuery)
		if err != nil {
			query = r.URL.RawQuery
		}

		target += "?" + strings.ToLower(query)
	}

	if target != "" && matchAny(opts.AttackPatterns, target) {
		return "attack payload"
	}

	return ""
}

// banner holds what Banner's ban step needs.
type banner struct {
	opts       BannerOptions
	logger     *zap.Logger
	notifier   notification.Notifier
	historyTTL time.Duration
}

// ban bans key for its next ban duration and returns when the ban ends (Unix seconds).
func (b *banner) ban(ctx *gin.Context, ip, key, reason string) int64 {
	level, err := b.opts.Cache.Incr(ctx, levelKey(key), b.historyTTL)
	if err != nil || level < 1 {
		level = 1
	}

	d := b.opts.BanDurations[min(int(level), len(b.opts.BanDurations))-1]
	until := time.Now().Add(d).Unix()
	if err := b.opts.Cache.Set(ctx, banKey(key), until, d); err != nil {
		b.logger.Error("middleware: banner ban failed", zap.String("ip", ip), zap.Error(err))
	}

	b.logger.Warn("middleware: client banned",
		zap.String("ip", ip),
		zap.String("blocked", key),
		zap.String("reason", reason),
		zap.String("path", ctx.Request.URL.Path),
		zap.Duration("duration", d),
	)

	if b.notifier != nil {
		msg := banAlertMessage(ctx, ip, key, reason, d)
		go b.notifier.SendMessageHTML(ctx.Copy(), msg, "error")
	}

	return until
}

// deny aborts with 403 and a Retry-After header for a ban ending at until (Unix seconds).
func deny(ctx *gin.Context, responder response.Responder, until int64) {
	if wait := until - time.Now().Unix(); wait > 0 {
		ctx.Header("Retry-After", strconv.FormatInt(wait, 10))
	}

	responder.Send(ctx, http.StatusForbidden, "access_denied")
	ctx.Abort()
}

// banKey returns the cache key of a client's ban.
func banKey(key string) string {
	return "banner:ban:" + key
}

// strikeKey returns the cache key of a client's strike count.
func strikeKey(key string) string {
	return "banner:strikes:" + key
}

// levelKey returns the cache key of a client's ban count.
func levelKey(key string) string {
	return "banner:level:" + key
}

// banAlertMessage formats an HTML alert for a new ban; request values are escaped since Telegram
// rejects (and so drops) messages with stray markup.
func banAlertMessage(ctx *gin.Context, ip, key, reason string, d time.Duration) string {
	return fmt.Sprintf(`
	⛔ <b>Client Banned</b>

	<b>Request Info</b>
	• <b>IP:</b> <code>%s</code> (blocked: <code>%s</code>)
	• <b>Reason:</b> %s
	• <b>Method:</b> <code>%s</code>
	• <b>Path:</b> <code>%s</code>
	• <b>Duration:</b> <code>%s</code>
	• <b>Time:</b> <code>%s</code>
	`,
		html.EscapeString(ip),
		html.EscapeString(key),
		html.EscapeString(reason),
		html.EscapeString(ctx.Request.Method),
		html.EscapeString(ctx.Request.URL.Path),
		d,
		time.Now().Format(time.RFC3339),
	)
}
