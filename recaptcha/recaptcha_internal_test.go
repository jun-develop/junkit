package recaptcha

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: recaptcha_internal_test.go
// Purpose: Verify Client.Verify against a fake siteverify server, the request it sends, and client defaults.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 14
//
// #   Test Case                                        Scenario                            Expected Result
// 1   TestClient_Verify_MapsResponseToError            Success with a high score           Returns nil
// 2   TestClient_Verify_MapsResponseToError            success=false from Google           Returns ErrVerificationFailed
// 3   TestClient_Verify_MapsResponseToError            Score below MinScore                Returns ErrLowScore
// 4   TestClient_Verify_MapsResponseToError            Non-200 status                      Returns ErrUnavailable
// 5   TestClient_Verify_MapsResponseToError            Invalid JSON body                   Returns ErrUnavailable
// 6   TestClient_Verify_MapsResponseToError            Action matches ExpectedAction       Returns nil
// 7   TestClient_Verify_MapsResponseToError            Action differs from ExpectedAction  Returns ErrVerificationFailed
// 8   TestClient_Verify_MapsResponseToError            Hostname in the allowed list        Returns nil
// 9   TestClient_Verify_MapsResponseToError            Hostname not in the allowed list    Returns ErrVerificationFailed
// 10  TestClient_Verify_SendsFormBody                  Verify with token and remote IP     POST form body with secret, response and remoteip; empty query
// 11  TestClient_Verify_TransportErrorHidesURL         Server unreachable, secret in URL   Returns ErrUnavailable without the URL or secret
// 12  TestNewClient_AppliesDefaults                    MinScore 0, nil HTTP client         MinScore 0.5, 10s-timeout client, Google endpoint
// 13  TestNewClient_AppliesDefaults                    Negative MinScore, nil HTTP client  MinScore 0.5, 10s-timeout client, Google endpoint
// 14  TestClient_Verify_MissingSecretKeyIsUnavailable  No secret key configured            Returns ErrUnavailable
//
// =============================================================================

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

func TestClient_Verify_MapsResponseToError(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		handler http.HandlerFunc
		wantErr error
	}{
		{"success with a high score", Options{}, reply(`{"success": true, "score": 0.9}`), nil},
		{"google rejects the token", Options{}, reply(`{"success": false, "error-codes": ["invalid-input-response"]}`),
			ErrVerificationFailed},
		{"score below min score", Options{}, reply(`{"success": true, "score": 0.1}`), ErrLowScore},
		{"non-200 status", Options{}, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, ErrUnavailable},
		{"invalid json body", Options{}, reply(`{`), ErrUnavailable},
		{"action matches expected action", Options{ExpectedAction: "login"},
			reply(`{"success": true, "score": 0.9, "action": "login"}`), nil},
		{"action differs from expected action", Options{ExpectedAction: "login"},
			reply(`{"success": true, "score": 0.9, "action": "signup"}`), ErrVerificationFailed},
		{"hostname in the allowed list", Options{Hostnames: []string{"a.example", "b.example"}},
			reply(`{"success": true, "score": 0.9, "hostname": "b.example"}`), nil},
		{"hostname not in the allowed list", Options{Hostnames: []string{"a.example"}},
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
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", gotType)
	}

	if gotSecret != "test-secret" {
		t.Errorf("form secret = %q, want %q", gotSecret, "test-secret")
	}

	if gotResponse != "tok" {
		t.Errorf("form response = %q, want %q", gotResponse, "tok")
	}

	if gotRemoteIP != "203.0.113.5" {
		t.Errorf("form remoteip = %q, want %q", gotRemoteIP, "203.0.113.5")
	}
}

func TestClient_Verify_TransportErrorHidesURL(t *testing.T) {
	c := NewClient(Options{SecretKey: "s3cr3t"}, nil)
	c.apiURL = "http://127.0.0.1:1/siteverify?secret=s3cr3t"

	err := c.Verify(context.Background(), "token", "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Verify() error = %v, want ErrUnavailable", err)
	}

	if strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("Verify() error = %q, want it without the secret", err)
	}

	if strings.Contains(err.Error(), "siteverify") {
		t.Errorf("Verify() error = %q, want it without the request URL", err)
	}
}

func TestNewClient_AppliesDefaults(t *testing.T) {
	tests := []struct {
		name     string
		minScore float64
	}{
		{"zero min score", 0},
		{"negative min score", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(Options{SecretKey: "x", MinScore: tt.minScore}, nil)

			if c.opts.MinScore != 0.5 {
				t.Errorf("MinScore = %v, want 0.5", c.opts.MinScore)
			}

			if c.httpClient == nil {
				t.Fatal("httpClient = nil, want a default client")
			}

			if c.httpClient.Timeout != 10*time.Second {
				t.Errorf("httpClient.Timeout = %v, want 10s", c.httpClient.Timeout)
			}

			if c.apiURL != siteVerifyURL {
				t.Errorf("apiURL = %q, want Google's siteverify endpoint", c.apiURL)
			}
		})
	}
}

func TestClient_Verify_MissingSecretKeyIsUnavailable(t *testing.T) {
	c := NewClient(Options{}, nil)

	if err := c.Verify(context.Background(), "token", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Verify() error = %v, want ErrUnavailable", err)
	}
}
