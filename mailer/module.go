package mailer

import (
	"fmt"
	"net/http"
	"time"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Driver selects which Sender implementation New constructs.
type Driver string

// Driver values accepted by New.
const (
	// DriverSMTP sends through any SMTP server (Brevo, Resend, SES, Mailgun, Postmark, ...).
	DriverSMTP Driver = "smtp"
	// DriverBrevo sends through the Brevo HTTP API.
	DriverBrevo Driver = "brevo"
	// DriverLog only logs each message; for local development and tests.
	DriverLog Driver = "log"
)

// Options selects and configures a Sender.
type Options struct {
	// Driver selects the provider (empty value defaults to DriverLog).
	Driver Driver

	// From is the default sender, e.g. "Jungo <no-reply@example.com>"; Message.From overrides it.
	From string

	// SMTP configures DriverSMTP.
	SMTP SMTPOptions

	// Brevo configures DriverBrevo.
	Brevo BrevoOptions
}

// SMTPOptions configures an SMTP server connection.
type SMTPOptions struct {
	// Host is the SMTP server, e.g. "smtp-relay.brevo.com".
	Host string

	// Port is the SMTP port. 465 uses implicit TLS; any other port upgrades with STARTTLS when the
	// server offers it. Default 587.
	Port int

	// Username and Password enable PLAIN auth; leave both empty for an unauthenticated relay.
	Username string
	Password string

	// Timeout bounds one whole send when ctx has no deadline. Default 30s.
	Timeout time.Duration
}

// BrevoOptions configures the Brevo HTTP API.
type BrevoOptions struct {
	// APIKey is a Brevo v3 API key.
	APIKey string
}

// New builds the Sender selected by opts.Driver. httpClient is used by HTTP API drivers; nil falls
// back to a client with a 30s timeout. An unknown driver is an error rather than a silent no-op so
// a typo in config never drops mail.
//
// Usage:
//
//	sender, err := mailer.New(mailer.Options{
//	    Driver: mailer.DriverSMTP,
//	    From:   "Jungo <no-reply@example.com>",
//	    SMTP:   mailer.SMTPOptions{Host: "smtp-relay.brevo.com", Username: user, Password: key},
//	}, nil, logger)
func New(opts Options, httpClient *http.Client, logger *zap.Logger) (Sender, error) {
	if opts.Driver != "" && opts.Driver != DriverLog {
		if _, err := parseAddress("from", opts.From); err != nil {
			return nil, err
		}
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	switch opts.Driver {
	case DriverSMTP:
		return newSMTPSender(opts.From, opts.SMTP)
	case DriverBrevo:
		return newBrevoSender(opts.From, opts.Brevo, httpClient)
	case "", DriverLog:
		return newLogSender(opts.From, logger), nil
	default:
		return nil, fmt.Errorf("mailer: unknown driver %q", opts.Driver)
	}
}

// params are New's Fx dependencies; the HTTP client is optional so SMTP needs no httpclient.Module.
type params struct {
	fx.In

	Opts       Options
	HTTPClient *http.Client `optional:"true"`
	Logger     *zap.Logger
}

// Module provides an Fx provider for Sender.
//
// Usage:
//
//	fx.New(
//	    fx.Supply(mailer.Options{Driver: mailer.DriverBrevo, From: from, Brevo: mailer.BrevoOptions{APIKey: key}}),
//	    mailer.Module,
//	)
var Module = fx.Module("mailer",
	fx.Provide(func(p params) (Sender, error) {
		return New(p.Opts, p.HTTPClient, p.Logger)
	}),
)
