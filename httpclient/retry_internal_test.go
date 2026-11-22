package httpclient

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: retry_internal_test.go
// Purpose: Verify the retry delay, retries on network errors, and forwarding of
//          CloseIdleConnections to the wrapped transport.
// Type: Unit
//
// Test functions: 3
// Total scenarios: 10
//
// #  Test Case                                  Scenario                       Expected Result
// 1  TestRetryTransport_DelayStaysWithinBounds  First attempt, no Retry-After  Between 100ms and 120ms (waitMin + jitter)
// 2  TestRetryTransport_DelayStaysWithinBounds  Attempt 10, no Retry-After     Exactly waitMax (1s), jitter included
// 3  TestRetryTransport_DelayStaysWithinBounds  Retry-After: 1                 1s
// 4  TestRetryTransport_DelayStaysWithinBounds  Retry-After: 120               Capped at waitMax (1s)
// 5  TestRetryTransport_DelayStaysWithinBounds  Retry-After date in the past   0
// 6  TestRetryTransport_DelayStaysWithinBounds  Retry-After: soon (invalid)    Falls back to backoff: 100ms to 120ms
// 7  TestRetryTransport_RoundTrip_RetriesNetworkErrors  One network error, then 200  Retries once and returns the 200
// 8  TestRetryTransport_RoundTrip_RetriesNetworkErrors  Network error on every attempt  Gives up after maxRetries with the last error
// 9  TestRetryTransport_CloseIdleConnections_ForwardsToBase  Base transport can close idle connections  Base CloseIdleConnections is called once
// 10  TestRetryTransport_CloseIdleConnections_ForwardsToBase  Base transport cannot close idle connections  Does nothing and does not panic
//
// =============================================================================

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// scriptedTransport returns errs[i] on call i (or a 200 once errs runs out) and counts calls,
// including calls to CloseIdleConnections.
type scriptedTransport struct {
	errs       []error
	calls      int
	closeCalls int
}

func (s *scriptedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	s.calls++
	if s.calls <= len(s.errs) {
		return nil, s.errs[s.calls-1]
	}

	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
}

func (s *scriptedTransport) CloseIdleConnections() { s.closeCalls++ }

// roundTripOnly is a RoundTripper without a CloseIdleConnections method.
type roundTripOnly struct{}

func (roundTripOnly) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

// httptestRequest returns a GET request, which is safe to retry.
func httptestRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}

	return req
}

func TestRetryTransport_DelayStaysWithinBounds(t *testing.T) {
	tr := &retryTransport{waitMin: 100 * time.Millisecond, waitMax: time.Second}
	tests := []struct {
		name       string
		attempt    int
		retryAfter string
		wantMin    time.Duration
		wantMax    time.Duration
	}{
		{"first backoff", 0, "", 100 * time.Millisecond, 120 * time.Millisecond},
		{"backoff capped including jitter", 10, "", time.Second, time.Second},
		{"retry-after in seconds", 0, "1", time.Second, time.Second},
		{"retry-after capped at waitMax", 0, "120", time.Second, time.Second},
		{"retry-after date in the past", 0, "Mon, 02 Jan 2006 15:04:05 GMT", 0, 0},
		{"invalid retry-after falls back to backoff", 0, "soon", 100 * time.Millisecond, 120 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tt.retryAfter != "" {
				resp.Header.Set("Retry-After", tt.retryAfter)
			}

			for range 50 {
				if got := tr.delay(tt.attempt, resp); got < tt.wantMin || got > tt.wantMax {
					t.Fatalf("delay() = %v, want within [%v, %v]", got, tt.wantMin, tt.wantMax)
				}
			}
		})
	}
}

func TestRetryTransport_RoundTrip_RetriesNetworkErrors(t *testing.T) {
	errNetwork := errors.New("connection reset")

	t.Run("one network error, then 200", func(t *testing.T) {
		base := &scriptedTransport{errs: []error{errNetwork}}
		tr := &retryTransport{base: base, maxRetries: 2, waitMin: time.Millisecond, waitMax: time.Millisecond}

		resp, err := tr.RoundTrip(httptestRequest(t))

		if err != nil {
			t.Fatalf("RoundTrip() error = %v, want nil after the retry", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		if base.calls != 2 {
			t.Errorf("base calls = %d, want 2 (first attempt + 1 retry)", base.calls)
		}
	})

	t.Run("network error on every attempt", func(t *testing.T) {
		base := &scriptedTransport{errs: []error{errNetwork, errNetwork, errNetwork}}
		tr := &retryTransport{base: base, maxRetries: 2, waitMin: time.Millisecond, waitMax: time.Millisecond}

		_, err := tr.RoundTrip(httptestRequest(t))

		if !errors.Is(err, errNetwork) {
			t.Errorf("RoundTrip() error = %v, want %v", err, errNetwork)
		}

		if base.calls != 3 {
			t.Errorf("base calls = %d, want 3 (first attempt + maxRetries)", base.calls)
		}
	})
}

func TestRetryTransport_CloseIdleConnections_ForwardsToBase(t *testing.T) {
	t.Run("base transport can close idle connections", func(t *testing.T) {
		base := &scriptedTransport{}
		tr := &retryTransport{base: base}

		tr.CloseIdleConnections()

		if base.closeCalls != 1 {
			t.Errorf("base CloseIdleConnections calls = %d, want 1", base.closeCalls)
		}
	})

	t.Run("base transport cannot close idle connections", func(t *testing.T) {
		tr := &retryTransport{base: roundTripOnly{}}

		tr.CloseIdleConnections()
	})
}
