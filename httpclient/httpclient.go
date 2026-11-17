// Package httpclient builds an HTTP client with connection pooling, timeouts, and automatic retry.
package httpclient

import (
	"net"
	"net/http"
	"time"
)

// Options configures an HTTP client instance.
type Options struct {
	// Timeout bounds an entire request (connect + any redirects + read body). Default 15s.
	Timeout time.Duration

	// MaxIdleConns caps idle connections across all hosts. Default 100.
	MaxIdleConns int

	// MaxIdleConnsPerHost caps idle connections to a single host. Default 20.
	MaxIdleConnsPerHost int

	// IdleConnTimeout is how long an idle connection is kept before closing. Default 90s.
	IdleConnTimeout time.Duration

	// DialTimeout bounds TCP connection establishment. Default 5s.
	DialTimeout time.Duration

	// TLSHandshakeTimeout bounds the TLS handshake. Default 5s.
	TLSHandshakeTimeout time.Duration

	// ResponseHeaderTimeout bounds waiting for response headers after the request is sent.
	// Default 10s.
	ResponseHeaderTimeout time.Duration

	// MaxRetries is the maximum number of retry attempts (0 disables retries). Only idempotent
	// methods, or requests with an Idempotency-Key header, are retried, and only when their body
	// can be replayed through Request.GetBody.
	MaxRetries int

	// RetryWaitMin is the backoff before the first retry. Default 200ms.
	RetryWaitMin time.Duration

	// RetryWaitMax caps every wait between retries, including jitter and a server's Retry-After.
	// Default 5s.
	RetryWaitMax time.Duration
}

// withDefaults returns a copy of o with every zero-valued field replaced by
// its documented default.
func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = 15 * time.Second
	}

	if o.MaxIdleConns <= 0 {
		o.MaxIdleConns = 100
	}

	if o.MaxIdleConnsPerHost <= 0 {
		o.MaxIdleConnsPerHost = 20
	}

	if o.IdleConnTimeout <= 0 {
		o.IdleConnTimeout = 90 * time.Second
	}

	if o.DialTimeout <= 0 {
		o.DialTimeout = 5 * time.Second
	}

	if o.TLSHandshakeTimeout <= 0 {
		o.TLSHandshakeTimeout = 5 * time.Second
	}

	if o.ResponseHeaderTimeout <= 0 {
		o.ResponseHeaderTimeout = 10 * time.Second
	}

	if o.RetryWaitMin <= 0 {
		o.RetryWaitMin = 200 * time.Millisecond
	}

	if o.RetryWaitMax <= 0 {
		o.RetryWaitMax = 5 * time.Second
	}

	return o
}

// NewClient builds an *http.Client configured by opts.
//
// Usage:
//
//	client := httpclient.NewClient(httpclient.Options{MaxRetries: 3})
//	resp, err := client.Get("https://api.example.com")
func NewClient(opts Options) *http.Client {
	opts = opts.withDefaults()

	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,

		MaxIdleConns:        opts.MaxIdleConns,
		MaxIdleConnsPerHost: opts.MaxIdleConnsPerHost,
		IdleConnTimeout:     opts.IdleConnTimeout,

		DialContext: (&net.Dialer{
			Timeout:   opts.DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   opts.TLSHandshakeTimeout,
		ResponseHeaderTimeout: opts.ResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,

		ForceAttemptHTTP2: true,
	}

	var transport http.RoundTripper = base
	if opts.MaxRetries > 0 {
		transport = &retryTransport{
			base:       base,
			maxRetries: opts.MaxRetries,
			waitMin:    opts.RetryWaitMin,
			waitMax:    opts.RetryWaitMax,
		}
	}

	return &http.Client{
		Timeout:   opts.Timeout,
		Transport: transport,
	}
}

// NewDefaultClient builds an *http.Client using default settings.
func NewDefaultClient() *http.Client {
	return NewClient(Options{})
}
