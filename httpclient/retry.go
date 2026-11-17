package httpclient

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// maxDrainBytes caps how much of a discarded response is read so its connection can be reused;
// larger bodies are closed instead, which is cheaper than reading them.
const maxDrainBytes = 4 << 10

// retryTransport wraps an http.RoundTripper to retry transient failures (network errors, HTTP
// 429 and 5xx) with exponential backoff and jitter. Only requests that are safe to replay are
// retried: idempotent methods, or any method carrying an Idempotency-Key header, and only when the
// body is absent or replayable through GetBody.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	waitMin    time.Duration
	waitMax    time.Duration
}

// RoundTrip implements http.RoundTripper. It never modifies req: each retry sends a clone with a
// fresh body from req.GetBody.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !replayable(req) {
		return t.base.RoundTrip(req)
	}

	attemptReq := req
	for attempt := 0; ; attempt++ {
		resp, err := t.base.RoundTrip(attemptReq)
		if attempt >= t.maxRetries || !shouldRetry(resp, err) {
			return resp, err
		}

		wait := t.delay(attempt, resp)
		if resp != nil {
			drainAndClose(resp.Body)
		}

		if err := sleepWithContext(req.Context(), wait); err != nil {
			return nil, err
		}

		attemptReq, err = cloneWithBody(req)
		if err != nil {
			return nil, err
		}
	}
}

// CloseIdleConnections forwards to the base transport so http.Client.CloseIdleConnections works
// through the retry wrapper.
func (t *retryTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// replayable reports whether req may be sent more than once.
func replayable(req *http.Request) bool {
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return false
	}

	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPut, http.MethodDelete:
		return true
	}

	return req.Header.Get("Idempotency-Key") != ""
}

// cloneWithBody returns a copy of req with a fresh body for the next attempt.
func cloneWithBody(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.GetBody == nil {
		return clone, nil
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}

	clone.Body = body

	return clone, nil
}

// shouldRetry reports whether a network error, 429 or 5xx response should be retried.
func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}

	return resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= http.StatusInternalServerError
}

// delay returns the wait before the next attempt: the server's Retry-After when present,
// otherwise exponential backoff with jitter; either way capped at waitMax.
func (t *retryTransport) delay(attempt int, resp *http.Response) time.Duration {
	if d, ok := retryAfter(resp); ok {
		return min(d, t.waitMax)
	}

	wait := t.waitMin << attempt
	if wait <= 0 || wait > t.waitMax {
		wait = t.waitMax
	}

	wait += time.Duration(rand.Int63n(int64(wait)/5 + 1))

	return min(wait, t.waitMax)
}

// retryAfter parses a Retry-After header given in seconds or as an HTTP date.
func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}

	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}

	if at, err := http.ParseTime(v); err == nil {
		return max(time.Until(at), 0), true
	}

	return 0, false
}

// drainAndClose reads a little of body so the connection can be reused, then closes it.
func drainAndClose(body io.ReadCloser) {
	// Best effort: a failed drain only costs connection reuse.
	_, _ = io.CopyN(io.Discard, body, maxDrainBytes)
	_ = body.Close()
}

// sleepWithContext waits for d, returning ctx.Err() early if ctx is done first.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
