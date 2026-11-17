package recaptcha

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient returns a Client pointed at a test server running handler.
func newTestClient(t *testing.T, opts Options, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	opts.SecretKey = "test-secret"
	c := NewClient(opts, server.Client())
	c.apiURL = server.URL

	return c
}

// reply returns a handler that writes body as the siteverify answer.
func reply(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}
}

func TestClient_Verify(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		handler http.HandlerFunc
		wantErr error
	}{
		{"success", Options{}, reply(`{"success": true, "score": 0.9}`), nil},
		{"rejected", Options{}, reply(`{"success": false, "error-codes": ["invalid-input-response"]}`),
			ErrVerificationFailed},
		{"low score", Options{}, reply(`{"success": true, "score": 0.1}`), ErrLowScore},
		{"non-200", Options{}, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, ErrUnavailable},
		{"bad json", Options{}, reply(`{`), ErrUnavailable},
		{"action match", Options{ExpectedAction: "login"},
			reply(`{"success": true, "score": 0.9, "action": "login"}`), nil},
		{"action mismatch", Options{ExpectedAction: "login"},
			reply(`{"success": true, "score": 0.9, "action": "signup"}`), ErrVerificationFailed},
		{"hostname match", Options{Hostnames: []string{"a.example", "b.example"}},
			reply(`{"success": true, "score": 0.9, "hostname": "b.example"}`), nil},
		{"hostname mismatch", Options{Hostnames: []string{"a.example"}},
			reply(`{"success": true, "score": 0.9, "hostname": "evil.example"}`), ErrVerificationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.opts, tt.handler)

			err := c.Verify(context.Background(), "token", "")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Verify_SendsFormBody(t *testing.T) {
	var gotQuery, gotType, gotSecret, gotResponse, gotRemoteIP string
	c := newTestClient(t, Options{}, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		gotType = r.Header.Get("Content-Type")
		gotSecret = r.PostFormValue("secret")
		gotResponse = r.PostFormValue("response")
		gotRemoteIP = r.PostFormValue("remoteip")
		_, _ = w.Write([]byte(`{"success": true, "score": 0.9}`))
	})

	if err := c.Verify(context.Background(), "tok", "203.0.113.5"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if gotQuery != "" {
		t.Errorf("query = %q, want empty so the secret stays out of URLs", gotQuery)
	}

	if gotType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotType)
	}

	if gotSecret != "test-secret" || gotResponse != "tok" || gotRemoteIP != "203.0.113.5" {
		t.Errorf("form = %q/%q/%q", gotSecret, gotResponse, gotRemoteIP)
	}
}

func TestClient_Verify_TransportErrorHidesURL(t *testing.T) {
	c := NewClient(Options{SecretKey: "s3cr3t"}, nil)
	c.apiURL = "http://127.0.0.1:1/siteverify?secret=s3cr3t"

	err := c.Verify(context.Background(), "token", "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Verify() error = %v, want ErrUnavailable", err)
	}

	if strings.Contains(err.Error(), "s3cr3t") || strings.Contains(err.Error(), "siteverify") {
		t.Fatalf("error %q leaks the request URL", err)
	}
}

func TestNewClient_Defaults(t *testing.T) {
	tests := []struct {
		name     string
		minScore float64
	}{
		{"zero", 0},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(Options{SecretKey: "x", MinScore: tt.minScore}, nil)

			if c.opts.MinScore != 0.5 {
				t.Errorf("MinScore = %v, want 0.5", c.opts.MinScore)
			}

			if c.httpClient == nil || c.httpClient.Timeout != 10*time.Second {
				t.Error("nil httpClient should default to a client with a 10s timeout")
			}

			if c.apiURL != siteVerifyURL {
				t.Errorf("apiURL = %q, want Google's siteverify endpoint", c.apiURL)
			}
		})
	}
}

func TestClient_Verify_MissingSecretKey(t *testing.T) {
	c := NewClient(Options{}, nil)

	if err := c.Verify(context.Background(), "token", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Verify() error = %v, want ErrUnavailable", err)
	}
}
