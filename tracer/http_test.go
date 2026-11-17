package tracer_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/tracer"
)

// TestServerInfo_MasksSecretsAndReportsRequestDetails checks details and masking.
func TestServerInfo_MasksSecretsAndReportsRequestDetails(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/1?foo=bar&token=abc", nil)
	r.Host = "api.example.com:8080"
	r.Header.Set("Authorization", "Bearer abcdefghijklmnopqrstuvwxyz")
	r.Header.Set("Cookie", "session=super-secret; theme=dark")
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	r.Header.Set("User-Agent", "test-agent")

	info := tracer.ServerInfo(r)

	if info["method"] != http.MethodGet {
		t.Errorf(`info["method"] = %v, want %q`, info["method"], http.MethodGet)
	}

	if info["path"] != "/users/1" {
		t.Errorf(`info["path"] = %v, want %q`, info["path"], "/users/1")
	}

	if info["query_string"] != "foo=bar&token=****" {
		t.Errorf(`info["query_string"] = %v, want %q`, info["query_string"], "foo=bar&token=****")
	}

	if want := "http://api.example.com:8080/users/1?foo=bar&token=****"; info["full_url"] != want {
		t.Errorf(`info["full_url"] = %v, want %q`, info["full_url"], want)
	}

	if info["host"] != "api.example.com" || info["host_port"] != "8080" {
		t.Errorf(`info["host"], info["host_port"] = %v, %v, want "api.example.com", "8080"`, info["host"], info["host_port"])
	}

	if info["client_ip"] != "192.0.2.1" {
		t.Errorf(`info["client_ip"] = %v, want %q (RemoteAddr; X-Forwarded-For is client-controlled)`, info["client_ip"], "192.0.2.1")
	}

	if info["user_agent"] != "test-agent" {
		t.Errorf(`info["user_agent"] = %v, want %q`, info["user_agent"], "test-agent")
	}

	auth, _ := info["authorization"].(string)
	if auth == "Bearer abcdefghijklmnopqrstuvwxyz" {
		t.Error("authorization header should be masked, not passed through verbatim")
	}

	cookie, _ := info["cookie"].(string)
	if cookie == "session=super-secret; theme=dark" {
		t.Error("cookie header should be masked, not passed through verbatim")
	}
}

// TestSplitHostPort checks splitHostPort with and without a port.
func TestSplitHostPort(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		wantHost string
		wantPort string
	}{
		{name: "host with port", addr: "example.com:8080", wantHost: "example.com", wantPort: "8080"},
		{name: "bare host, no port", addr: "example.com", wantHost: "example.com", wantPort: ""},
		{name: "IPv4 with port", addr: "127.0.0.1:9000", wantHost: "127.0.0.1", wantPort: "9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Host = tt.addr
			info := tracer.ServerInfo(r)

			if info["host"] != tt.wantHost {
				t.Errorf(`info["host"] = %v, want %q`, info["host"], tt.wantHost)
			}

			if info["host_port"] != tt.wantPort {
				t.Errorf(`info["host_port"] = %v, want %q`, info["host_port"], tt.wantPort)
			}
		})
	}
}

// TestHTTPInfo checks HTTPInfo records one info entry and is a no-op without a Debugger.
func TestHTTPInfo(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)

	// No Debugger: must not panic.
	tracer.HTTPInfo(r.Context(), r)

	d := tracer.New()
	tracer.HTTPInfo(tracer.WithContext(r.Context(), d), r)

	logs := d.Logs()
	if len(logs) != 1 || logs[0].Type != tracer.TypeVariable || logs[0].Label != "REQUEST & SERVER INFO" {
		t.Fatalf("Logs() = %+v, want one REQUEST & SERVER INFO variable entry", logs)
	}
}

// TestHTTPInfo_GinContextUsesClientIP checks trusted-proxy handling.
func TestHTTPInfo_GinContextUsesClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, engine := gin.CreateTestContext(httptest.NewRecorder())
	// No trusted proxy: the spoofed headers must be ignored.
	if err := engine.SetTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}

	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("X-Real-IP", "6.6.6.6")
	c.Request.Header.Set("X-Forwarded-For", "6.6.6.6")

	d := tracer.New()
	c.Request = c.Request.WithContext(tracer.WithContext(c.Request.Context(), d))
	tracer.HTTPInfo(c, c.Request)

	info := d.Logs()[0].Data.(map[string]any)
	if info["client_ip"] != c.ClientIP() || info["client_ip"] != "192.0.2.1" {
		t.Fatalf(`client_ip = %v, want gin's ClientIP %q`, info["client_ip"], c.ClientIP())
	}
}
