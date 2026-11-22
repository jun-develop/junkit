package httpclient_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: httpclient_test.go
// Purpose: Verify client defaults, retry behavior against a local test server, and the fx module.
// Type: Unit (uses httptest servers)
//
// Test functions: 11
// Total scenarios: 14
//
// #   Test Case                                        Scenario                          Expected Result
// 1   TestNewClient_AppliesDefaults                    Empty Options                     15s timeout, plain transport, 100 idle conns, 20 per host
// 2   TestNewClient_RespectsExplicitOptions            Timeout set to 3s                 Client timeout is 3s
// 3   TestNewDefaultClient_UsesDefaultsWithoutRetries  No arguments                      15s timeout, plain transport (no retries)
// 4   TestClient_RetriesOnServerErrorThenSucceeds      Two 503s, then 200                Returns 200 after 3 attempts
// 5   TestClient_GivesUpAfterMaxRetries                Always 500, MaxRetries 2          Returns the last 500 after 3 attempts
// 6   TestClient_DoesNotRetryOnOrdinaryClientError     Server returns 400                Exactly 1 attempt
// 7   TestClient_RetriesOnTooManyRequests              One 429, then 200                 Returns 200
// 8   TestClient_RequestBodyIsReplayedAcrossRetries    PUT with a body, two 503s         Every attempt receives the full body
// 9   TestClient_StopsRetryingWhenContextIsCanceled    Context times out during backoff  Returns an error promptly
// 10  TestClient_RetriesOnlyReplayableRequests         GET                               Retried: 3 attempts
// 11  TestClient_RetriesOnlyReplayableRequests         POST                              Not retried: 1 attempt
// 12  TestClient_RetriesOnlyReplayableRequests         POST with Idempotency-Key         Retried: 3 attempts
// 13  TestClient_RetriesOnlyReplayableRequests         PUT with a non-replayable body    Not retried: 1 attempt; request left untouched
// 14  TestModule_ProvidesClientAndStopsCleanly         fx app start and stop             Provides an *http.Client; stop hook runs without error
//
// =============================================================================

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jun-develop/junkit/httpclient"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestNewClient_AppliesDefaults(t *testing.T) {
	client := httpclient.NewClient(httpclient.Options{})

	if client.Timeout != 15*time.Second {
		t.Fatalf("Timeout = %v, want the documented default of 15s", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport when MaxRetries is 0", client.Transport)
	}

	if transport.MaxIdleConns != 100 {
		t.Fatalf("MaxIdleConns = %d, want the documented default of 100", transport.MaxIdleConns)
	}

	if transport.MaxIdleConnsPerHost != 20 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want the documented default of 20", transport.MaxIdleConnsPerHost)
	}
}

func TestNewClient_RespectsExplicitOptions(t *testing.T) {
	client := httpclient.NewClient(httpclient.Options{Timeout: 3 * time.Second})
	if client.Timeout != 3*time.Second {
		t.Fatalf("Timeout = %v, want %v", client.Timeout, 3*time.Second)
	}
}

func TestNewDefaultClient_UsesDefaultsWithoutRetries(t *testing.T) {
	client := httpclient.NewDefaultClient()
	if client.Timeout != 15*time.Second {
		t.Fatalf("Timeout = %v, want the documented default of 15s", client.Timeout)
	}

	if _, ok := client.Transport.(*http.Transport); !ok {
		t.Fatalf("Transport = %T, want *http.Transport (retries disabled)", client.Transport)
	}
}

func TestClient_RetriesOnServerErrorThenSucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	defer server.Close()

	client := httpclient.NewClient(httpclient.Options{
		MaxRetries:   3,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
	})

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("final status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("server received %d attempts, want 3 (2 failures + 1 success)", got)
	}
}

func TestClient_GivesUpAfterMaxRetries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))

	defer server.Close()

	client := httpclient.NewClient(httpclient.Options{
		MaxRetries:   2,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
	})

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("final status = %d, want %d (last attempt's response, returned after exhausting retries)", resp.StatusCode, http.StatusInternalServerError)
	}

	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("server received %d attempts, want 3 (1 initial + 2 retries)", got)
	}
}

func TestClient_DoesNotRetryOnOrdinaryClientError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))

	defer server.Close()

	client := httpclient.NewClient(httpclient.Options{
		MaxRetries:   3,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
	})

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	defer resp.Body.Close()

	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("server received %d attempts, want 1 — a plain 400 should never be retried", got)
	}
}

func TestClient_RetriesOnTooManyRequests(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	defer server.Close()

	client := httpclient.NewClient(httpclient.Options{
		MaxRetries:   2,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
	})

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("final status = %d, want %d after a 429 retry", resp.StatusCode, http.StatusOK)
	}
}

func TestClient_RequestBodyIsReplayedAcrossRetries(t *testing.T) {
	var attempts int32
	var receivedBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		receivedBodies = append(receivedBodies, string(body))

		if atomic.AddInt32(&attempts, 1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))

	defer server.Close()

	client := httpclient.NewClient(httpclient.Options{
		MaxRetries:   3,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
	})

	req, err := http.NewRequest(http.MethodPut, server.URL, strings.NewReader("request payload"))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	defer resp.Body.Close()

	if len(receivedBodies) != 3 {
		t.Fatalf("server saw %d requests, want 3", len(receivedBodies))
	}

	for i, body := range receivedBodies {
		if body != "request payload" {
			t.Fatalf("attempt %d body = %q, want the original body replayed intact", i+1, body)
		}
	}
}

func TestClient_StopsRetryingWhenContextIsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	defer server.Close()

	client := httpclient.NewClient(httpclient.Options{
		MaxRetries:   10,
		RetryWaitMin: 50 * time.Millisecond,
		RetryWaitMax: time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext() error = %v", err)
	}

	start := time.Now()
	_, err = client.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Do() error = nil, want an error once the context deadline is exceeded")
	}

	if elapsed > 2*time.Second {
		t.Fatalf("Do() took %v, want it to stop promptly once the context was canceled (not run all 10 retries)", elapsed)
	}
}

// opaqueReader hides strings.Reader so http.NewRequest cannot set GetBody.
type opaqueReader struct{ io.Reader }

func TestClient_RetriesOnlyReplayableRequests(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		body         func() io.Reader
		idempotency  bool
		wantAttempts int32
	}{
		{"GET is retried", http.MethodGet, nil, false, 3},
		{"POST is not retried", http.MethodPost, func() io.Reader { return strings.NewReader("x") }, false, 1},
		{"POST with Idempotency-Key is retried", http.MethodPost,
			func() io.Reader { return strings.NewReader("x") }, true, 3},
		{"PUT with a non-replayable body is not retried", http.MethodPut,
			func() io.Reader { return opaqueReader{strings.NewReader("x")} }, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&attempts, 1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)

			client := httpclient.NewClient(httpclient.Options{
				MaxRetries:   2,
				RetryWaitMin: time.Millisecond,
				RetryWaitMax: 5 * time.Millisecond,
			})

			var body io.Reader
			if tt.body != nil {
				body = tt.body()
			}

			req, err := http.NewRequest(tt.method, server.URL, body)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}

			if tt.idempotency {
				req.Header.Set("Idempotency-Key", "k1")
			}

			getBody := req.GetBody
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}

			resp.Body.Close()

			if got := atomic.LoadInt32(&attempts); got != tt.wantAttempts {
				t.Fatalf("attempts = %d, want %d", got, tt.wantAttempts)
			}

			if (getBody == nil) != (req.GetBody == nil) {
				t.Fatal("req.GetBody changed after Do(), want RoundTrip to leave the caller's request untouched")
			}
		})
	}
}

func TestModule_ProvidesClientAndStopsCleanly(t *testing.T) {
	var client *http.Client
	app := fxtest.New(t,
		fx.Supply(httpclient.Options{}),
		httpclient.Module,
		fx.Populate(&client),
	)
	app.RequireStart().RequireStop()

	if client == nil {
		t.Fatal("Module did not provide an *http.Client")
	}
}
