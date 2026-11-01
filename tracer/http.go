package tracer

import (
	"context"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getHostname returns the current machine's hostname, or "unknown" if it can't be determined.
func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}

	return h
}

// getServerIP returns the first non-loopback IPv4 address on this machine, or "unknown".
func getServerIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}

	return "unknown"
}

// splitHostPort splits addr into host and port, tolerating a bare host with no port.
func splitHostPort(addr string) (host, port string) {
	if !strings.ContainsRune(addr, ':') {
		return addr, ""
	}

	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, ""
	}

	return h, p
}

// ServerInfo collects request and server environment details for debug tracing.
func ServerInfo(r *http.Request) map[string]any {
	host, port := splitHostPort(r.Host)
	_, clientPort := splitHostPort(r.RemoteAddr)

	return map[string]any{
		"timestamp":       time.Now().UnixMilli(),
		"method":          r.Method,
		"full_url":        rebuildFullURL(r),
		"path":            r.URL.Path,
		"query_string":    r.URL.RawQuery,
		"protocol":        r.Proto,
		"host":            host,
		"host_port":       port,
		"server_name":     getHostname(),
		"server_ip":       getServerIP(),
		"client_ip":       clientHost(r),
		"client_port":     clientPort,
		"x_forwarded_for": r.Header.Get("X-Forwarded-For"),
		"x_real_ip":       r.Header.Get("X-Real-IP"),
		"user_agent":      r.UserAgent(),
		"referer":         r.Referer(),
		"content_type":    r.Header.Get("Content-Type"),
		"authorization":   maskAuthorization(r.Header.Get("Authorization")),
		"cookie":          maskCookie(r.Header.Get("Cookie")),
		"headers":         cloneHeaders(r.Header),
		"go_version":      runtime.Version(),
		"pid":             os.Getpid(),
		"goroutines":      runtime.NumGoroutine(),
	}
}

// HTTPInfo records HTTP request and server info on the ctx Debugger. When ctx is a *gin.Context,
// client_ip is its ClientIP, which honours the engine's trusted-proxy settings.
func HTTPInfo(ctx context.Context, r *http.Request) {
	if d := FromContext(ctx); d != nil {
		info := ServerInfo(r)
		if gc, ok := ctx.(*gin.Context); ok {
			info["client_ip"] = gc.ClientIP()
		}

		d.record(TypeVariable, "REQUEST & SERVER INFO", info)
	}
}

// clientHost returns the host of r.RemoteAddr; proxy headers are client-controlled, so they are
// only shown raw (x_forwarded_for, x_real_ip), never trusted here.
func clientHost(r *http.Request) string {
	host, _ := splitHostPort(r.RemoteAddr)

	return host
}

// rebuildFullURL reconstructs r's absolute URL, honoring TLS/X-Forwarded-Proto for scheme.
func rebuildFullURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	return scheme + "://" + r.Host + r.RequestURI
}

// maskAuthorization keeps only the auth scheme (e.g. "Bearer") and hides the credentials.
func maskAuthorization(s string) string {
	if s == "" {
		return ""
	}

	if scheme, _, ok := strings.Cut(s, " "); ok {
		return scheme + " ****"
	}

	return "****"
}

// maskCookie replaces every cookie value with "****", keeping the cookie names visible.
func maskCookie(s string) string {
	if s == "" {
		return ""
	}

	parts := strings.Split(s, "; ")
	for i, p := range parts {
		if eq := strings.IndexByte(p, '='); eq > 0 {
			parts[i] = p[:eq+1] + "****"
		}
	}

	return strings.Join(parts, "; ")
}

// secretHeaderHints are substrings marking a header name as carrying a secret.
var secretHeaderHints = []string{"token", "secret", "password", "api-key", "apikey", "signature", "session"}

// MaskHeader returns value with any secret it carries, as judged by the header name, hidden.
func MaskHeader(name, value string) string {
	lower := strings.ToLower(name)
	switch lower {
	case "authorization", "proxy-authorization":
		return maskAuthorization(value)
	case "cookie", "set-cookie":
		return maskCookie(value)
	}

	for _, hint := range secretHeaderHints {
		if strings.Contains(lower, hint) {
			return "****"
		}
	}

	return value
}

// cloneHeaders deep-copies h, masking every value of secret-bearing headers.
func cloneHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}

	clone := make(http.Header, len(h))
	for k, vv := range h {
		masked := make([]string, len(vv))
		for i, v := range vv {
			masked[i] = MaskHeader(k, v)
		}

		clone[k] = masked
	}

	return clone
}
