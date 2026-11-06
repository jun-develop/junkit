package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// smtpSender implements Sender over SMTP.
type smtpSender struct {
	from string
	opts SMTPOptions
}

// newSMTPSender creates an smtpSender, applying SMTPOptions defaults.
func newSMTPSender(from string, opts SMTPOptions) (*smtpSender, error) {
	if opts.Host == "" {
		return nil, fmt.Errorf("mailer: smtp host is required")
	}

	if opts.Port == 0 {
		opts.Port = 587
	}

	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	return &smtpSender{from: from, opts: opts}, nil
}

// Send implements Sender; it opens one connection per message.
func (s *smtpSender) Send(ctx context.Context, msg Message) error {
	env, err := prepare(msg, s.from)
	if err != nil {
		return err
	}

	raw, err := buildMIME(env, time.Now())
	if err != nil {
		return err
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.opts.Timeout)
		defer cancel()
	}

	conn, err := s.dial(ctx)
	if err != nil {
		return fmt.Errorf("mailer: smtp dial: %w", err)
	}

	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() }) // unblock I/O on cancel
	defer stop()

	c, err := smtp.NewClient(conn, s.opts.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mailer: smtp handshake: %w", err)
	}

	defer c.Close()

	if s.opts.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.opts.Host}); err != nil {
				return fmt.Errorf("mailer: smtp starttls: %w", err)
			}
		}
	}

	if s.opts.Username != "" {
		// PlainAuth refuses to send credentials over a non-TLS connection (except to localhost).
		if err := c.Auth(smtp.PlainAuth("", s.opts.Username, s.opts.Password, s.opts.Host)); err != nil {
			return fmt.Errorf("mailer: smtp auth: %w", err)
		}
	}

	if err := c.Mail(env.from.Address); err != nil {
		return fmt.Errorf("mailer: smtp MAIL FROM: %w", err)
	}

	for _, r := range env.recipients() {
		if err := c.Rcpt(r.Address); err != nil {
			return fmt.Errorf("mailer: smtp RCPT TO %s: %w", r.Address, err)
		}
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mailer: smtp DATA: %w", err)
	}

	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("mailer: smtp write: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("mailer: smtp send: %w", err)
	}

	_ = c.Quit()

	return nil
}

// dial connects to the server, using implicit TLS on port 465.
func (s *smtpSender) dial(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(s.opts.Host, strconv.Itoa(s.opts.Port))
	if s.opts.Port == 465 {
		d := &tls.Dialer{Config: &tls.Config{ServerName: s.opts.Host}}
		return d.DialContext(ctx, "tcp", addr)
	}

	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// buildMIME renders env as an RFC 5322 message. Bcc is deliberately not written as a header.
func buildMIME(env *envelope, now time.Time) ([]byte, error) {
	var buf bytes.Buffer

	header := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }

	header("From", env.from.String())
	if len(env.to) > 0 {
		header("To", joinAddresses(env.to))
	}

	if len(env.cc) > 0 {
		header("Cc", joinAddresses(env.cc))
	}

	if env.replyTo != nil {
		header("Reply-To", env.replyTo.String())
	}

	header("Subject", mime.QEncoding.Encode("utf-8", env.msg.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", uuid.NewString(), domainOf(env.from.Address)))
	header("MIME-Version", "1.0")

	msg := env.msg
	bodyHeader, body, err := bodyPart(msg.HTML, msg.Text)
	if err != nil {
		return nil, err
	}

	if len(msg.Attachments) == 0 {
		for k := range bodyHeader {
			header(k, bodyHeader.Get(k))
		}

		buf.WriteString("\r\n")
		buf.Write(body)
		return buf.Bytes(), nil
	}

	mixed := multipart.NewWriter(&buf)
	header("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mixed.Boundary()}))
	buf.WriteString("\r\n")

	part, err := mixed.CreatePart(bodyHeader)
	if err != nil {
		return nil, err
	}

	if _, err := part.Write(body); err != nil {
		return nil, err
	}

	for _, a := range msg.Attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}

		h := textproto.MIMEHeader{}
		h.Set("Content-Type", ct)
		h.Set("Content-Transfer-Encoding", "base64")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename}))

		part, err := mixed.CreatePart(h)
		if err != nil {
			return nil, err
		}

		if err := writeBase64Lines(part, a.Data); err != nil {
			return nil, err
		}
	}

	if err := mixed.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// bodyPart renders the message body and its headers: a single part when only one of html/text is
// set, multipart/alternative when both are.
func bodyPart(html, text string) (textproto.MIMEHeader, []byte, error) {
	h := textproto.MIMEHeader{}
	var buf bytes.Buffer

	if html == "" || text == "" {
		ct, body := "text/plain; charset=utf-8", text
		if html != "" {
			ct, body = "text/html; charset=utf-8", html
		}

		h.Set("Content-Type", ct)
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		if err := writeQP(&buf, body); err != nil {
			return nil, nil, err
		}

		return h, buf.Bytes(), nil
	}

	alt := multipart.NewWriter(&buf)
	h.Set("Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alt.Boundary()}))

	for _, p := range []struct{ ct, body string }{
		{"text/plain; charset=utf-8", text},
		{"text/html; charset=utf-8", html},
	} {
		ph := textproto.MIMEHeader{}
		ph.Set("Content-Type", p.ct)
		ph.Set("Content-Transfer-Encoding", "quoted-printable")

		part, err := alt.CreatePart(ph)
		if err != nil {
			return nil, nil, err
		}

		if err := writeQP(part, p.body); err != nil {
			return nil, nil, err
		}
	}

	if err := alt.Close(); err != nil {
		return nil, nil, err
	}

	return h, buf.Bytes(), nil
}

// writeQP writes s quoted-printable encoded, which keeps lines under SMTP's 998-byte limit.
func writeQP(w io.Writer, s string) error {
	qp := quotedprintable.NewWriter(w)
	if _, err := qp.Write([]byte(s)); err != nil {
		return err
	}

	return qp.Close()
}

// writeBase64Lines writes data base64 encoded in 76-character lines.
func writeBase64Lines(w io.Writer, data []byte) error {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 76 {
		if _, err := io.WriteString(w, enc[:76]+"\r\n"); err != nil {
			return err
		}

		enc = enc[76:]
	}

	_, err := io.WriteString(w, enc+"\r\n")
	return err
}

func joinAddresses(list []*mail.Address) string {
	parts := make([]string, len(list))
	for i, a := range list {
		parts[i] = a.String()
	}

	return strings.Join(parts, ", ")
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return addr[i+1:]
	}

	return "localhost"
}
