// Package mailer sends transactional email through a pluggable provider (SMTP, Brevo API, or log).
package mailer

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

// Sender sends one email. Consumers depend on this interface only, so switching provider is a
// config change (Options.Driver), never a code change.
type Sender interface {
	Send(ctx context.Context, msg Message) error
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

// ErrInvalidMessage is returned (wrapped) when a Message fails validation before sending.
var ErrInvalidMessage = errors.New("mailer: invalid message")

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
		if a.Filename == "" || strings.ContainsAny(a.Filename, "\r\n") {
			return nil, fmt.Errorf("%w: invalid attachment filename %q", ErrInvalidMessage, a.Filename)
		}
	}

	return env, nil
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
