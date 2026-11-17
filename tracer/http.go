package tracer

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/security"
)

// hostname is the machine's hostname, or "unknown", looked up once since it rarely changes.
var hostname = sync.OnceValue(func() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}

	return h
})

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
		"query_string":    maskQuery(r.URL.RawQuery),
		"protocol":        r.Proto,
		"host":            host,
		"host_port":       port,
		"server_name":     hostname(),
		"server_ip":       getServerIP(),
		"client_ip":       clientHost(r),
		"client_port":     clientPort,
		"x_forwarded_for": r.Header.Get("X-Forwarded-For"),
		"x_real_ip":       r.Header.Get("X-Real-IP"),
		"user_agent":      r.UserAgent(),
		"referer":         r.Referer(),
		"content_type":    r.Header.Get("Content-Type"),
		"authorization":   security.MaskHeader("Authorization", r.Header.Get("Authorization")),
		"cookie":          security.MaskHeader("Cookie", r.Header.Get("Cookie")),
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

// rebuildFullURL reconstructs r's absolute URL, honoring TLS/X-Forwarded-Proto for scheme, with
// secret query parameters masked.
func rebuildFullURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	uri := r.RequestURI
	if path, query, ok := strings.Cut(uri, "?"); ok {
		uri = path + "?" + maskQuery(query)
	}

	return scheme + "://" + r.Host + uri
}

// maskQuery replaces the value of every secret-named parameter in the raw query with "****",
// keeping parameter order and the encoding of the rest untouched.
func maskQuery(raw string) string {
	params := strings.Split(raw, "&")
	for i, p := range params {
		rawName, _, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}

		name, err := url.QueryUnescape(rawName)
		if err != nil {
			name = rawName
		}

		if security.IsSecretName(name) {
			params[i] = rawName + "=****"
		}
	}

	return strings.Join(params, "&")
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
			masked[i] = security.MaskHeader(k, v)
		}

		clone[k] = masked
	}

	return clone
}
