// Package recaptcha verifies Google reCAPTCHA v3 tokens.
//
// Verify returns nil only for an accepted token; otherwise the error wraps ErrVerificationFailed,
// ErrLowScore or ErrUnavailable so callers can tell a bad token from an outage with errors.Is.
package recaptcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// siteVerifyURL is Google's token verification endpoint.
const siteVerifyURL = "https://www.google.com/recaptcha/api/siteverify"

// maxResponseBytes caps how much of the siteverify response is read; real answers are tiny.
const maxResponseBytes = 64 << 10

// Verifier verifies reCAPTCHA v3 tokens. Verify returns nil only when Google accepts token and
// its score meets the minimum; remoteIP is optional and sent only when non-empty.
type Verifier interface {
	Verify(ctx context.Context, token string, remoteIP string) error
}

// Options configures reCAPTCHA token verification.
type Options struct {
	// SecretKey is the reCAPTCHA secret key issued by Google for your site.
	SecretKey string

	// MinScore is the minimum acceptable reCAPTCHA v3 score (0.0-1.0, defaults to 0.5 when <= 0).
	MinScore float64

	// ExpectedAction, when set, must equal the action the token was issued for.
	ExpectedAction string

	// Hostnames, when non-empty, lists the site hostnames a token may come from.
	Hostnames []string

	// Mock, when true, enables MockVerifier for local development and tests.
	Mock bool
}

// withDefaults returns a copy of o with zero values replaced by defaults.
func (o Options) withDefaults() Options {
	if o.MinScore <= 0 {
		o.MinScore = 0.5
	}

	return o
}

// Client verifies reCAPTCHA tokens against Google's API.
type Client struct {
	opts       Options
	httpClient *http.Client
	apiURL     string
}

// siteVerifyResponse is Google's siteverify JSON response shape.
type siteVerifyResponse struct {
	Success     bool      `json:"success"`
	Score       float64   `json:"score"`
	Action      string    `json:"action"`
	ChallengeTS time.Time `json:"challenge_ts"`
	Hostname    string    `json:"hostname"`
	ErrorCodes  []string  `json:"error-codes"`
}

// NewClient creates a Client; a nil httpClient falls back to a client with a 10s timeout.
//
// Usage:
//
//	c := recaptcha.NewClient(recaptcha.Options{SecretKey: secret, ExpectedAction: "login"}, nil)
func NewClient(opts Options, httpClient *http.Client) *Client {
	opts = opts.withDefaults()
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Client{
		opts:       opts,
		httpClient: httpClient,
		apiURL:     siteVerifyURL,
	}
}

// Verify implements Verifier; it posts the token to Google's siteverify endpoint.
func (c *Client) Verify(ctx context.Context, token string, remoteIP string) error {
	if c.opts.SecretKey == "" {
		return fmt.Errorf("%w: secret key not configured", ErrUnavailable)
	}

	verifyResp, err := c.siteVerify(ctx, token, remoteIP)
	if err != nil {
		return err
	}

	return c.check(verifyResp)
}

// siteVerify sends the form-encoded request so the secret never appears in a URL, and returns
// errors stripped of the request URL.
func (c *Client) siteVerify(ctx context.Context, token, remoteIP string) (*siteVerifyResponse, error) {
	form := url.Values{}
	form.Set("secret", c.opts.SecretKey)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: create request", ErrUnavailable)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// *url.Error embeds the request URL; keep only the underlying cause.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}

		return nil, fmt.Errorf("%w: send request: %w", ErrUnavailable, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %w", ErrUnavailable, err)
	}

	var verifyResp siteVerifyResponse
	if err := json.Unmarshal(body, &verifyResp); err != nil {
		return nil, fmt.Errorf("%w: decode response: %w", ErrUnavailable, err)
	}

	return &verifyResp, nil
}

// check applies success, action, hostname and score rules to a siteverify answer.
func (c *Client) check(r *siteVerifyResponse) error {
	if !r.Success {
		return fmt.Errorf("%w: %v", ErrVerificationFailed, r.ErrorCodes)
	}

	if c.opts.ExpectedAction != "" && r.Action != c.opts.ExpectedAction {
		return fmt.Errorf("%w: action %q, want %q", ErrVerificationFailed, r.Action,
			c.opts.ExpectedAction)
	}

	if len(c.opts.Hostnames) > 0 && !slices.Contains(c.opts.Hostnames, r.Hostname) {
		return fmt.Errorf("%w: unexpected hostname %q", ErrVerificationFailed, r.Hostname)
	}

	if r.Score < c.opts.MinScore {
		return fmt.Errorf("%w: %.2f < %.2f", ErrLowScore, r.Score, c.opts.MinScore)
	}

	return nil
}
