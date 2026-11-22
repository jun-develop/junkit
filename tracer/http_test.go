package tracer_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: http_test.go
// Purpose: Verify the request/server details that ServerInfo and HTTPInfo record.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 7
//
// #  Test Case                                            Scenario                                             Expected Result
// 1  TestServerInfo_MasksSecretsAndReportsRequestDetails  GET with secrets in query, Authorization and Cookie  Reports method, path, URL, host, port, RemoteAddr IP, user agent; secrets masked
// 2  TestServerInfo_SplitsHostAndPort                     Host with port                                       host and host_port split
// 3  TestServerInfo_SplitsHostAndPort                     Bare host, no port                                   host set, host_port empty
// 4  TestServerInfo_SplitsHostAndPort                     IPv4 with port                                       host and host_port split
// 5  TestHTTPInfo_RecordsRequestInfoEntry                 No Debugger                                          No panic
// 6  TestHTTPInfo_RecordsRequestInfoEntry                 Debugger attached                                    One "REQUEST & SERVER INFO" variable entry
// 7  TestHTTPInfo_GinContextUsesClientIP                  gin.Context, no trusted proxy, spoofed IP headers    client_ip is gin's ClientIP (RemoteAddr), not the spoofed IP
//
// =============================================================================

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/tracer"
)

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

	if info["host"] != "api.example.com" {
		t.Errorf(`info["host"] = %v, want %q`, info["host"], "api.example.com")
	}

	if info["host_port"] != "8080" {
		t.Errorf(`info["host_port"] = %v, want %q`, info["host_port"], "8080")
	}

	if info["client_ip"] != "192.0.2.1" {
		t.Errorf(`info["client_ip"] = %v, want %q (RemoteAddr; X-Forwarded-For is client-controlled)`, info["client_ip"], "192.0.2.1")
	}

	if info["user_agent"] != "test-agent" {
		t.Errorf(`info["user_agent"] = %v, want %q`, info["user_agent"], "test-agent")
	}

	if auth, _ := info["authorization"].(string); auth == "Bearer abcdefghijklmnopqrstuvwxyz" {
		t.Errorf(`info["authorization"] = %q, want it masked`, auth)
	}

	if cookie, _ := info["cookie"].(string); cookie == "session=super-secret; theme=dark" {
		t.Errorf(`info["cookie"] = %q, want it masked`, cookie)
	}
}

func TestServerInfo_SplitsHostAndPort(t *testing.T) {
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

func TestHTTPInfo_RecordsRequestInfoEntry(t *testing.T) {
	t.Run("no debugger", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)

		// Passes if this does not panic.
		tracer.HTTPInfo(r.Context(), r)
	})

	t.Run("debugger attached", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		d := tracer.New()

		tracer.HTTPInfo(tracer.WithContext(r.Context(), d), r)

		logs := d.Logs()
		if len(logs) != 1 {
			t.Fatalf("Logs() = %+v, want 1 entry", logs)
		}

		if logs[0].Type != tracer.TypeVariable {
			t.Errorf("entry Type = %v, want TypeVariable", logs[0].Type)
		}

		if logs[0].Label != "REQUEST & SERVER INFO" {
			t.Errorf("entry Label = %q, want %q", logs[0].Label, "REQUEST & SERVER INFO")
		}
	})
}

func TestHTTPInfo_GinContextUsesClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, engine := gin.CreateTestContext(httptest.NewRecorder())
	// No trusted proxy: the spoofed headers must be ignored.
	if err := engine.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies(nil) error = %v", err)
	}

	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("X-Real-IP", "6.6.6.6")
	c.Request.Header.Set("X-Forwarded-For", "6.6.6.6")

	d := tracer.New()
	c.Request = c.Request.WithContext(tracer.WithContext(c.Request.Context(), d))
	tracer.HTTPInfo(c, c.Request)

	logs := d.Logs()
	if len(logs) != 1 {
		t.Fatalf("Logs() = %+v, want 1 entry", logs)
	}

	info := logs[0].Data.(map[string]any)
	if info["client_ip"] != c.ClientIP() {
		t.Errorf("client_ip = %v, want gin's ClientIP %q", info["client_ip"], c.ClientIP())
	}

	if info["client_ip"] != "192.0.2.1" {
		t.Errorf("client_ip = %v, want the RemoteAddr %q, not the spoofed header", info["client_ip"], "192.0.2.1")
	}
}
