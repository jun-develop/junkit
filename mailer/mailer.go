// Package mailer sends transactional email through a pluggable provider (SMTP, Brevo API, or log).
package mailer

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Sender sends one email. Consumers depend on this interface only, so switching provider is a
// config change (Options.Driver), never a code change.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

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

// withDefaults returns a copy of o with zero values replaced by defaults.
func (o Options) withDefaults() Options {
	if o.Driver == "" {
		o.Driver = DriverLog
	}

	o.SMTP = o.SMTP.withDefaults()

	return o
}

// SMTPOptions configures an SMTP server connection.
type SMTPOptions struct {
	// Host is the SMTP server, e.g. "smtp-relay.brevo.com".
	Host string

	// Port is the SMTP port. 465 uses implicit TLS; any other port must upgrade with STARTTLS
	// unless AllowPlaintext is set. Default 587.
	Port int

	// Username and Password enable PLAIN auth; leave both empty for an unauthenticated relay.
	Username string
	Password string

	// AllowPlaintext permits sending without TLS when the server offers no STARTTLS. Only for
	// local relays such as Mailpit; otherwise Send fails with ErrTLSRequired.
	AllowPlaintext bool

	// Timeout bounds one whole send when ctx has no deadline. Default 30s.
	Timeout time.Duration
}

// withDefaults returns a copy of o with zero values replaced by defaults.
func (o SMTPOptions) withDefaults() SMTPOptions {
	if o.Port == 0 {
		o.Port = 587
	}

	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}

	return o
}

// BrevoOptions configures the Brevo HTTP API.
type BrevoOptions struct {
	// APIKey is a Brevo v3 API key.
	APIKey string
}

// New builds the Sender selected by opts.Driver. httpClient is used by HTTP API drivers; nil falls
// back to a client with a 10s timeout. An unknown driver is an error rather than a silent no-op so
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
	opts = opts.withDefaults()
	if opts.Driver != DriverLog {
		if _, err := parseAddress("from", opts.From); err != nil {
			return nil, err
		}
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	switch opts.Driver {
	case DriverSMTP:
		return newSMTPSender(opts.From, opts.SMTP)
	case DriverBrevo:
		return newBrevoSender(opts.From, opts.Brevo, httpClient)
	case DriverLog:
		return newLogSender(opts.From, logger), nil
	default:
		return nil, fmt.Errorf("mailer: unknown driver %q", opts.Driver)
	}
}

// Message is a provider-independent email. Addresses use RFC 5322 form: "a@x.com" or
// "Name <a@x.com>". At least one of To/Cc/Bcc and one of HTML/Text is required.
type Message struct {
	From    string
	To      []string
	Cc      []string
	Bcc     []string
	ReplyTo string
	Subject string
	HTML    string
	Text    string

	Attachments []Attachment
}

// Attachment is a file attached to a Message. An empty ContentType defaults to
// "application/octet-stream".
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// envelope is a validated Message with parsed addresses, shared by every driver.
type envelope struct {
	from    *mail.Address
	to      []*mail.Address
	cc      []*mail.Address
	bcc     []*mail.Address
	replyTo *mail.Address
	msg     Message
}

// recipients returns every envelope recipient (To, Cc and Bcc).
func (e *envelope) recipients() []*mail.Address {
	return append(append(append([]*mail.Address{}, e.to...), e.cc...), e.bcc...)
}

// prepare validates msg and parses its addresses, falling back to defaultFrom.
func prepare(msg Message, defaultFrom string) (*envelope, error) {
	from := msg.From
	if from == "" {
		from = defaultFrom
	}

	env := &envelope{msg: msg}

	var err error
	if env.from, err = parseAddress("from", from); err != nil {
		return nil, err
	}

	if env.to, err = parseList("to", msg.To); err != nil {
		return nil, err
	}

	if env.cc, err = parseList("cc", msg.Cc); err != nil {
		return nil, err
	}

	if env.bcc, err = parseList("bcc", msg.Bcc); err != nil {
		return nil, err
	}

	if msg.ReplyTo != "" {
		if env.replyTo, err = parseAddress("reply-to", msg.ReplyTo); err != nil {
			return nil, err
		}
	}

	if len(env.to)+len(env.cc)+len(env.bcc) == 0 {
		return nil, fmt.Errorf("%w: no recipients", ErrInvalidMessage)
	}

	if strings.ContainsAny(msg.Subject, "\r\n") {
		return nil, fmt.Errorf("%w: subject contains a line break", ErrInvalidMessage)
	}

	if msg.HTML == "" && msg.Text == "" {
		return nil, fmt.Errorf("%w: empty body", ErrInvalidMessage)
	}

	for _, a := range msg.Attachments {
		if err := validateAttachment(a); err != nil {
			return nil, err
		}
	}

	return env, nil
}

// validateAttachment rejects filenames and content types that could inject MIME headers.
func validateAttachment(a Attachment) error {
	if a.Filename == "" || strings.ContainsAny(a.Filename, "\r\n") {
		return fmt.Errorf("%w: invalid attachment filename %q", ErrInvalidMessage, a.Filename)
	}

	if a.ContentType == "" {
		return nil
	}

	if _, _, err := mime.ParseMediaType(a.ContentType); err != nil ||
		strings.ContainsAny(a.ContentType, "\r\n") {
		return fmt.Errorf("%w: invalid attachment content type %q", ErrInvalidMessage, a.ContentType)
	}

	return nil
}

// parseAddress parses one RFC 5322 address.
func parseAddress(field, s string) (*mail.Address, error) {
	a, err := mail.ParseAddress(s)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %q: %v", ErrInvalidMessage, field, s, err)
	}

	return a, nil
}

// parseList parses each address in list.
func parseList(field string, list []string) ([]*mail.Address, error) {
	out := make([]*mail.Address, 0, len(list))
	for _, s := range list {
		a, err := parseAddress(field, s)
		if err != nil {
			return nil, err
		}

		out = append(out, a)
	}

	return out, nil
}
