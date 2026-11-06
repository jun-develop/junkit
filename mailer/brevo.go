package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
)

// brevoEndpoint is Brevo's transactional send API.
const brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

// brevoSender implements Sender with the Brevo v3 HTTP API.
type brevoSender struct {
	from       string
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// newBrevoSender creates a brevoSender.
func newBrevoSender(from string, opts BrevoOptions, httpClient *http.Client) (*brevoSender, error) {
	if opts.APIKey == "" {
		return nil, fmt.Errorf("mailer: brevo api key is required")
	}

	return &brevoSender{from: from, apiKey: opts.APIKey, endpoint: brevoEndpoint, httpClient: httpClient}, nil
}

type brevoContact struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type brevoAttachment struct {
	Name    string `json:"name"`
	Content []byte `json:"content"` // encoding/json base64-encodes []byte, which Brevo expects
}

type brevoRequest struct {
	Sender      brevoContact      `json:"sender"`
	To          []brevoContact    `json:"to,omitempty"`
	Cc          []brevoContact    `json:"cc,omitempty"`
	Bcc         []brevoContact    `json:"bcc,omitempty"`
	ReplyTo     *brevoContact     `json:"replyTo,omitempty"`
	Subject     string            `json:"subject"`
	HTMLContent string            `json:"htmlContent,omitempty"`
	TextContent string            `json:"textContent,omitempty"`
	Attachment  []brevoAttachment `json:"attachment,omitempty"`
}

// Send implements Sender.
func (s *brevoSender) Send(ctx context.Context, msg Message) error {
	env, err := prepare(msg, s.from)
	if err != nil {
		return err
	}

	reqBody := brevoRequest{
		Sender:      toBrevo(env.from),
		To:          toBrevoList(env.to),
		Cc:          toBrevoList(env.cc),
		Bcc:         toBrevoList(env.bcc),
		Subject:     msg.Subject,
		HTMLContent: msg.HTML,
		TextContent: msg.Text,
	}

	if env.replyTo != nil {
		c := toBrevo(env.replyTo)
		reqBody.ReplyTo = &c
	}

	for _, a := range msg.Attachments {
		reqBody.Attachment = append(reqBody.Attachment, brevoAttachment{Name: a.Filename, Content: a.Data})
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("mailer: encode brevo request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mailer: build brevo request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mailer: brevo request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("mailer: brevo api error (status %d): %s", resp.StatusCode, body)
	}

	return nil
}

func toBrevo(a *mail.Address) brevoContact {
	return brevoContact{Email: a.Address, Name: a.Name}
}

func toBrevoList(list []*mail.Address) []brevoContact {
	out := make([]brevoContact, 0, len(list))
	for _, a := range list {
		out = append(out, toBrevo(a))
	}

	return out
}
