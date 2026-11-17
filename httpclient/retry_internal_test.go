package httpclient

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryTransport_Delay(t *testing.T) {
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
		{"retry-after seconds", 0, "1", time.Second, time.Second},
		{"retry-after capped", 0, "120", time.Second, time.Second},
		{"retry-after past date", 0, "Mon, 02 Jan 2006 15:04:05 GMT", 0, 0},
		{"invalid retry-after falls back", 0, "soon", 100 * time.Millisecond, 120 * time.Millisecond},
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
