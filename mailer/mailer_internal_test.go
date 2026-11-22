package mailer

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: mailer_internal_test.go
// Purpose: Verify message validation, MIME building, the Brevo, SMTP and log senders (including
//          server errors and TLS enforcement), and driver selection.
// Type: Unit (Brevo is an httptest server; SMTP is an in-process fake server on 127.0.0.1)
//
// Test functions: 18
// Total scenarios: 44
//
// #   Test Case                                                       Scenario                                           Expected Result
// 1   TestPrepare_RejectsInvalidMessage                               No recipients                                      Returns ErrInvalidMessage
// 2   TestPrepare_RejectsInvalidMessage                               Invalid To address                                 Returns ErrInvalidMessage
// 3   TestPrepare_RejectsInvalidMessage                               CRLF header injection in the subject               Returns ErrInvalidMessage
// 4   TestPrepare_RejectsInvalidMessage                               Empty HTML and text body                           Returns ErrInvalidMessage
// 5   TestPrepare_RejectsInvalidMessage                               Attachment without a filename                      Returns ErrInvalidMessage
// 6   TestPrepare_RejectsInvalidMessage                               Invalid From override                              Returns ErrInvalidMessage
// 7   TestPrepare_RejectsInvalidMessage                               CRLF injection in an attachment content type       Returns ErrInvalidMessage
// 8   TestPrepare_RejectsInvalidMessage                               Malformed attachment content type                  Returns ErrInvalidMessage
// 9   TestPrepare_AcceptsValidMessage                                 Valid message                                      Returns no error
// 10  TestBuildMIME_BuildsAlternativeBodyWithAttachment               HTML + text body and a UTF-8 attachment            multipart/mixed with text and HTML alternatives, encoded subject and filename, no Bcc header
// 11  TestBrevoSender_Send_PostsMessageToAPI                          Valid message                                      Request has the API key, sender, recipients and HTML
// 12  TestBrevoSender_Send_ReturnsAPIError                            API answers 401                                    Returns an error mentioning 401
// 13  TestSMTPSender_Send_SpeaksSMTP                                  Plaintext server allowed                           Transcript has MAIL FROM, RCPT TO for To and Bcc, encoded subject
// 14  TestSMTPSender_Send_SpeaksSMTP                                  Username and password set                          Transcript has AUTH PLAIN with the credentials
// 15  TestSMTPSender_Send_SpeaksSMTP                                  Server without STARTTLS, plaintext not allowed     Returns ErrTLSRequired
// 16  TestNew_RejectsInvalidOptions                                   Unknown driver                                     Returns an error
// 17  TestNew_RejectsInvalidOptions                                   SMTP driver with an invalid From                   Returns an error
// 18  TestNew_RejectsInvalidOptions                                   Brevo driver without an API key                    Returns an error
// 19  TestNew_DefaultsToLogDriver                                     Empty options                                      Returns a sender whose Send succeeds
// 20  TestSMTPSender_Send_ReturnsServerErrors                         Server rejects the connection greeting             Returns a "mailer: smtp handshake" error
// 21  TestSMTPSender_Send_ReturnsServerErrors                         Server rejects the credentials                     Returns a "mailer: smtp auth" error
// 22  TestSMTPSender_Send_ReturnsServerErrors                         Server rejects the sender                          Returns a "mailer: smtp MAIL FROM" error
// 23  TestSMTPSender_Send_ReturnsServerErrors                         Server rejects a recipient                         Returns a "mailer: smtp RCPT TO <address>" error
// 24  TestSMTPSender_Send_ReturnsServerErrors                         Server refuses the DATA command                    Returns a "mailer: smtp DATA" error
// 25  TestSMTPSender_Send_ReturnsServerErrors                         Server rejects the message after the body          Returns a "mailer: smtp send" error
// 26  TestSMTPSender_Send_NeverFallsBackToPlaintextWhenSTARTTLSFails  STARTTLS offered but fails, plaintext not allowed  "smtp starttls" error; no MAIL FROM sent
// 27  TestSMTPSender_Send_NeverFallsBackToPlaintextWhenSTARTTLSFails  STARTTLS offered but fails, plaintext allowed      "smtp starttls" error; no MAIL FROM sent
// 28  TestSMTPSender_Send_FailsWhenServerUnreachable                  Nothing listens on the port                        Returns a "mailer: smtp dial" error
// 29  TestBodyPart_RendersSinglePartBody                              Text only                                          text/plain, quoted-printable, decodes to the text
// 30  TestBodyPart_RendersSinglePartBody                              HTML only                                          text/html, quoted-printable, decodes to the HTML
// 31  TestBodyPart_RendersSinglePartBody                              2000-character line                                Encoded lines <= 76 chars; decodes to the original
// 32  TestWriteBase64Lines_WrapsAt76Characters                        Empty data                                         One empty line ending in CRLF
// 33  TestWriteBase64Lines_WrapsAt76Characters                        57 bytes                                           Exactly one 76-character line
// 34  TestWriteBase64Lines_WrapsAt76Characters                        58 bytes                                           A 76-character line plus a short one
// 35  TestWriteBase64Lines_WrapsAt76Characters                        300 bytes                                          Six lines, all full but the last; decodes back
// 36  TestDomainOf_ReturnsDomainOrLocalhost                           Plain address                                      Returns the domain
// 37  TestDomainOf_ReturnsDomainOrLocalhost                           Several @ signs                                    Returns the part after the last @
// 38  TestDomainOf_ReturnsDomainOrLocalhost                           No @ sign                                          Returns "localhost"
// 39  TestBrevoSender_Send_SendsCcReplyToAndAttachments               Message with Cc, Reply-To and an attachment        Request JSON has cc, replyTo and base64 attachment content
// 40  TestBrevoSender_Send_ReturnsErrorsWithoutLeakingDetails         Transport failure                                  Error is "mailer: brevo request: <cause>" without the URL
// 41  TestBrevoSender_Send_ReturnsErrorsWithoutLeakingDetails         API answers 500 with a 100 KB body                 Error mentions status 500; body capped at 4 KiB
// 42  TestBrevoSender_Send_ReturnsErrorsWithoutLeakingDetails         Invalid message                                    Returns ErrInvalidMessage; API not called
// 43  TestLogSender_Send_LogsMessageInsteadOfSending                  Empty From, text + HTML body                       One log entry: default dev sender, text body, To + Bcc
// 44  TestNewLogSender_NilLoggerIsSafe                                nil logger                                         Send returns no error
//
// =============================================================================

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"slices"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

// validMessage returns a Message that passes prepare.
func validMessage() Message {
	return Message{
		To:      []string{"Bob <bob@example.com>"},
		Bcc:     []string{"audit@example.com"},
		Subject: "Xin chào",
		HTML:    "<p>Hi</p>",
		Text:    "Hi",
	}
}

// failingTransport fails every HTTP request, like an unreachable API.
type failingTransport struct{}

// RoundTrip implements http.RoundTripper.
func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

// fakeSMTP is a minimal plaintext SMTP server recording one transaction; the transcript is sent
// on result when the connection ends. By default it accepts everything, including any AUTH.
// replies overrides the reply to a command ("MAIL", "RCPT", ...); "GREETING" overrides the
// greeting and "." the reply after the message body. extensions are extra EHLO keywords such as
// "STARTTLS" (the fake cannot actually start TLS).
func fakeSMTP(t *testing.T, replies map[string]string, extensions ...string) (port int, result <-chan []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error = %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan []string, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}

		defer conn.Close()
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		replyTo := func(cmd, fallback string) {
			if override, ok := replies[cmd]; ok {
				reply(override)
				return
			}

			reply(fallback)
		}

		var log []string
		defer func() { ch <- log }()

		replyTo("GREETING", "220 fake")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimRight(line, "\r\n")
			log = append(log, line)
			switch cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0]); cmd {
			case "EHLO", "HELO":
				reply("250-fake")
				for _, ext := range extensions {
					reply("250-" + ext)
				}

				reply("250 AUTH PLAIN")
			case "AUTH":
				replyTo(cmd, "235 ok")
			case "DATA":
				if override, ok := replies[cmd]; ok {
					reply(override)
					continue
				}

				reply("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}

					if l == ".\r\n" {
						break
					}

					log = append(log, strings.TrimRight(l, "\r\n"))
				}

				replyTo(".", "250 queued")
			case "QUIT":
				reply("221 bye")
				return
			default:
				replyTo(cmd, "250 ok")
			}
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, ch
}

func TestPrepare_RejectsInvalidMessage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Message)
	}{
		{"no recipients", func(m *Message) { m.To, m.Bcc = nil, nil }},
		{"invalid to address", func(m *Message) { m.To = []string{"not-an-email"} }},
		{"crlf header injection in the subject", func(m *Message) { m.Subject = "hi\r\nBcc: evil@example.com" }},
		{"empty html and text body", func(m *Message) { m.HTML, m.Text = "", "" }},
		{"attachment without a filename", func(m *Message) { m.Attachments = []Attachment{{Filename: ""}} }},
		{"invalid from override", func(m *Message) { m.From = "nope" }},
		{"crlf injection in an attachment content type", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "a.txt", ContentType: "text/plain\r\nBcc: evil@example.com"}}
		}},
		{"malformed attachment content type", func(m *Message) {
			m.Attachments = []Attachment{{Filename: "a.txt", ContentType: "text/"}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validMessage()
			tt.mutate(&m)

			if _, err := prepare(m, "App <app@example.com>"); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("prepare() error = %v, want ErrInvalidMessage", err)
			}
		})
	}
}

func TestPrepare_AcceptsValidMessage(t *testing.T) {
	if _, err := prepare(validMessage(), "App <app@example.com>"); err != nil {
		t.Fatalf("prepare(valid message) error = %v, want nil", err)
	}
}

func TestBuildMIME_BuildsAlternativeBodyWithAttachment(t *testing.T) {
	m := validMessage()
	m.Attachments = []Attachment{{Filename: "báo cáo.txt", ContentType: "text/plain", Data: []byte("data")}}
	env, err := prepare(m, "App <app@example.com>")
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}

	raw, err := buildMIME(env, time.Now())
	if err != nil {
		t.Fatalf("buildMIME() error = %v", err)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if bcc := parsed.Header.Get("Bcc"); bcc != "" {
		t.Fatalf("Bcc header = %q, want no Bcc header", bcc)
	}

	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil {
		t.Fatalf("decode Subject error = %v", err)
	}

	if subject != "Xin chào" {
		t.Fatalf("subject = %q, want %q", subject, "Xin chào")
	}

	mt, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse Content-Type error = %v", err)
	}

	if mt != "multipart/mixed" {
		t.Fatalf("Content-Type = %q, want multipart/mixed", mt)
	}

	mr := multipart.NewReader(parsed.Body, params["boundary"])
	body, err := mr.NextPart()
	if err != nil {
		t.Fatalf("read body part error = %v", err)
	}

	bt, bparams, err := mime.ParseMediaType(body.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse body Content-Type error = %v", err)
	}

	if bt != "multipart/alternative" {
		t.Fatalf("body Content-Type = %q, want multipart/alternative", bt)
	}

	alt := multipart.NewReader(body, bparams["boundary"])
	var texts []string
	for {
		p, err := alt.NextPart()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatalf("read alternative part error = %v", err)
		}

		// multipart.Reader decodes quoted-printable.
		b, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("read alternative content error = %v", err)
		}

		texts = append(texts, string(b))
	}

	if want := []string{"Hi", "<p>Hi</p>"}; !slices.Equal(texts, want) {
		t.Fatalf("alternatives = %q, want %q (text first, then HTML)", texts, want)
	}

	att, err := mr.NextPart()
	if err != nil {
		t.Fatalf("read attachment part error = %v", err)
	}

	if att.FileName() != "báo cáo.txt" {
		t.Fatalf("attachment filename = %q, want %q", att.FileName(), "báo cáo.txt")
	}
}

func TestBrevoSender_Send_PostsMessageToAPI(t *testing.T) {
	var got brevoRequest
	var apiKey string
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("api-key")
		decodeErr = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	s, err := newBrevoSender("App <app@example.com>", BrevoOptions{APIKey: "k"}, srv.Client())
	if err != nil {
		t.Fatalf("newBrevoSender() error = %v", err)
	}

	s.endpoint = srv.URL
	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if decodeErr != nil {
		t.Fatalf("decode request body error = %v", decodeErr)
	}

	if apiKey != "k" {
		t.Errorf("api-key header = %q, want %q", apiKey, "k")
	}

	if got.Sender.Email != "app@example.com" {
		t.Errorf("sender email = %q, want %q", got.Sender.Email, "app@example.com")
	}

	if len(got.To) != 1 || got.To[0].Name != "Bob" {
		t.Errorf("to = %+v, want one recipient named Bob", got.To)
	}

	if len(got.Bcc) != 1 || got.Bcc[0].Email != "audit@example.com" {
		t.Errorf("bcc = %+v, want audit@example.com", got.Bcc)
	}

	if got.HTMLContent != "<p>Hi</p>" {
		t.Errorf("htmlContent = %q, want %q", got.HTMLContent, "<p>Hi</p>")
	}
}

func TestBrevoSender_Send_ReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"unauthorized"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	s, err := newBrevoSender("app@example.com", BrevoOptions{APIKey: "k"}, srv.Client())
	if err != nil {
		t.Fatalf("newBrevoSender() error = %v", err)
	}

	s.endpoint = srv.URL

	err = s.Send(context.Background(), validMessage())

	if err == nil {
		t.Fatal("Send() error = nil, want an API error")
	}

	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("Send() error = %v, want it to mention status 401", err)
	}
}

func TestSMTPSender_Send_SpeaksSMTP(t *testing.T) {
	tests := []struct {
		name     string
		opts     SMTPOptions
		wantErr  error
		wantLogs []string
	}{
		{
			name: "plaintext server allowed",
			opts: SMTPOptions{AllowPlaintext: true},
			wantLogs: []string{"MAIL FROM:<app@example.com>", "RCPT TO:<bob@example.com>",
				"RCPT TO:<audit@example.com>", "Subject: =?utf-8?q?Xin_ch=C3=A0o?="},
		},
		{
			name: "username and password use auth plain",
			opts: SMTPOptions{AllowPlaintext: true, Username: "user", Password: "pass"},
			wantLogs: []string{"AUTH PLAIN " +
				base64.StdEncoding.EncodeToString([]byte("\x00user\x00pass"))},
		},
		{
			name:    "plaintext server rejected by default",
			opts:    SMTPOptions{},
			wantErr: ErrTLSRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, result := fakeSMTP(t, nil)
			tt.opts.Host, tt.opts.Port, tt.opts.Timeout = "127.0.0.1", port, 5*time.Second
			s, err := newSMTPSender("App <app@example.com>", tt.opts)
			if err != nil {
				t.Fatalf("newSMTPSender() error = %v", err)
			}

			err = s.Send(context.Background(), validMessage())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Send() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr != nil {
				return
			}

			log := strings.Join(<-result, "\n")
			for _, want := range tt.wantLogs {
				if !strings.Contains(log, want) {
					t.Fatalf("transcript missing %q:\n%s", want, log)
				}
			}
		})
	}
}

func TestNew_RejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"unknown driver", Options{Driver: "sendgridd", From: "a@example.com"}},
		{"smtp driver with an invalid from", Options{Driver: DriverSMTP, From: "bad"}},
		{"brevo driver without an api key", Options{Driver: DriverBrevo, From: "a@example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.opts, nil, zaptest.NewLogger(t)); err == nil {
				t.Fatalf("New(%+v) error = nil, want an error", tt.opts)
			}
		})
	}
}

func TestNew_DefaultsToLogDriver(t *testing.T) {
	s, err := New(Options{}, nil, zaptest.NewLogger(t))
	if err != nil {
		t.Fatalf("New(empty options) error = %v", err)
	}

	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("log driver Send() error = %v", err)
	}
}

func TestSMTPSender_Send_ReturnsServerErrors(t *testing.T) {
	tests := []struct {
		name    string
		opts    SMTPOptions
		replies map[string]string
		wantErr string
	}{
		{"server rejects the connection greeting", SMTPOptions{AllowPlaintext: true},
			map[string]string{"GREETING": "554 go away"}, "mailer: smtp handshake"},
		{"server rejects the credentials", SMTPOptions{AllowPlaintext: true, Username: "user", Password: "wrong"},
			map[string]string{"AUTH": "535 bad credentials"}, "mailer: smtp auth"},
		{"server rejects the sender", SMTPOptions{AllowPlaintext: true},
			map[string]string{"MAIL": "550 sender refused"}, "mailer: smtp MAIL FROM"},
		{"server rejects a recipient", SMTPOptions{AllowPlaintext: true},
			map[string]string{"RCPT": "550 no such user"}, "mailer: smtp RCPT TO bob@example.com"},
		{"server refuses the DATA command", SMTPOptions{AllowPlaintext: true},
			map[string]string{"DATA": "554 no data"}, "mailer: smtp DATA"},
		{"server rejects the message after the body", SMTPOptions{AllowPlaintext: true},
			map[string]string{".": "554 spam detected"}, "mailer: smtp send"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, _ := fakeSMTP(t, tt.replies)
			tt.opts.Host, tt.opts.Port, tt.opts.Timeout = "127.0.0.1", port, 5*time.Second
			s, err := newSMTPSender("App <app@example.com>", tt.opts)
			if err != nil {
				t.Fatalf("newSMTPSender() error = %v", err)
			}

			err = s.Send(context.Background(), validMessage())

			if err == nil {
				t.Fatalf("Send() error = nil, want an error starting with %q", tt.wantErr)
			}

			if !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Errorf("Send() error = %q, want it to start with %q", err, tt.wantErr)
			}
		})
	}
}

func TestSMTPSender_Send_NeverFallsBackToPlaintextWhenSTARTTLSFails(t *testing.T) {
	tests := []struct {
		name string
		opts SMTPOptions
	}{
		{"plaintext not allowed", SMTPOptions{}},
		{"plaintext allowed", SMTPOptions{AllowPlaintext: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The fake advertises STARTTLS but answers the command with 250, so the upgrade fails.
			port, result := fakeSMTP(t, nil, "STARTTLS")
			tt.opts.Host, tt.opts.Port, tt.opts.Timeout = "127.0.0.1", port, 5*time.Second
			s, err := newSMTPSender("App <app@example.com>", tt.opts)
			if err != nil {
				t.Fatalf("newSMTPSender() error = %v", err)
			}

			err = s.Send(context.Background(), validMessage())

			if err == nil {
				t.Fatal("Send() error = nil, want a \"mailer: smtp starttls\" error")
			}

			if !strings.HasPrefix(err.Error(), "mailer: smtp starttls") {
				t.Fatalf("Send() error = %q, want it to start with %q", err, "mailer: smtp starttls")
			}

			transcript := strings.Join(<-result, "\n")
			if strings.Contains(transcript, "MAIL FROM") {
				t.Errorf("transcript contains MAIL FROM, want no message sent without TLS:\n%s", transcript)
			}
		})
	}
}

func TestSMTPSender_Send_FailsWhenServerUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen error = %v", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener error = %v", err)
	}

	s, err := newSMTPSender("App <app@example.com>", SMTPOptions{Host: "127.0.0.1", Port: port, AllowPlaintext: true, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("newSMTPSender() error = %v", err)
	}

	err = s.Send(context.Background(), validMessage())

	if err == nil {
		t.Fatal("Send() error = nil, want a \"mailer: smtp dial\" error")
	}

	if !strings.HasPrefix(err.Error(), "mailer: smtp dial") {
		t.Fatalf("Send() error = %q, want it to start with %q", err, "mailer: smtp dial")
	}
}

func TestBodyPart_RendersSinglePartBody(t *testing.T) {
	longLine := strings.Repeat("a", 2000)
	tests := []struct {
		name            string
		html            string
		text            string
		wantContentType string
		wantBody        string
	}{
		{"text only", "", "Hi", "text/plain; charset=utf-8", "Hi"},
		{"html only", "<p>Hi</p>", "", "text/html; charset=utf-8", "<p>Hi</p>"},
		{"2000-character line", "", longLine, "text/plain; charset=utf-8", longLine},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, body, err := bodyPart(tt.html, tt.text)
			if err != nil {
				t.Fatalf("bodyPart() error = %v", err)
			}

			if got := header.Get("Content-Type"); got != tt.wantContentType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantContentType)
			}

			if got := header.Get("Content-Transfer-Encoding"); got != "quoted-printable" {
				t.Errorf("Content-Transfer-Encoding = %q, want quoted-printable", got)
			}

			for _, line := range strings.Split(string(body), "\r\n") {
				if len(line) > 76 {
					t.Fatalf("encoded line length = %d, want <= 76 (SMTP limits line length)", len(line))
				}
			}

			decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
			if err != nil {
				t.Fatalf("decode quoted-printable error = %v", err)
			}

			if string(decoded) != tt.wantBody {
				t.Errorf("decoded body = %q, want %q", decoded, tt.wantBody)
			}
		})
	}
}

func TestWriteBase64Lines_WrapsAt76Characters(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantLines int
	}{
		{"empty data", nil, 1},
		{"57 bytes fill exactly one line", bytes.Repeat([]byte{0xab}, 57), 1},
		{"58 bytes spill into a second line", bytes.Repeat([]byte{0xab}, 58), 2},
		{"300 bytes", bytes.Repeat([]byte{0xab}, 300), 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			if err := writeBase64Lines(&buf, tt.data); err != nil {
				t.Fatalf("writeBase64Lines() error = %v", err)
			}

			out := buf.String()
			if !strings.HasSuffix(out, "\r\n") {
				t.Fatalf("output = %q, want it to end with CRLF", out)
			}

			lines := strings.Split(strings.TrimSuffix(out, "\r\n"), "\r\n")
			if len(lines) != tt.wantLines {
				t.Fatalf("line count = %d, want %d", len(lines), tt.wantLines)
			}

			for i, line := range lines[:len(lines)-1] {
				if len(line) != 76 {
					t.Errorf("line %d length = %d, want 76", i+1, len(line))
				}
			}

			if last := lines[len(lines)-1]; len(last) > 76 {
				t.Errorf("last line length = %d, want <= 76", len(last))
			}

			decoded, err := base64.StdEncoding.DecodeString(strings.Join(lines, ""))
			if err != nil {
				t.Fatalf("decode base64 error = %v", err)
			}

			if !bytes.Equal(decoded, tt.data) {
				t.Errorf("decoded data = %x, want %x", decoded, tt.data)
			}
		})
	}
}

func TestDomainOf_ReturnsDomainOrLocalhost(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{"plain address", "app@example.com", "example.com"},
		{"several @ signs use the last one", "a@b@mail.example.com", "mail.example.com"},
		{"no @ sign", "app", "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domainOf(tt.addr); got != tt.want {
				t.Errorf("domainOf(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestBrevoSender_Send_SendsCcReplyToAndAttachments(t *testing.T) {
	var body []byte
	var readErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	s, err := newBrevoSender("App <app@example.com>", BrevoOptions{APIKey: "k"}, srv.Client())
	if err != nil {
		t.Fatalf("newBrevoSender() error = %v", err)
	}

	s.endpoint = srv.URL
	m := validMessage()
	m.Cc = []string{"carol@example.com"}
	m.ReplyTo = "Support <support@example.com>"
	m.Attachments = []Attachment{{Filename: "a.txt", ContentType: "text/plain", Data: []byte("data")}}

	if err := s.Send(context.Background(), m); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if readErr != nil {
		t.Fatalf("read request body error = %v", readErr)
	}

	for _, want := range []string{
		`"cc":[{"email":"carol@example.com"}]`,
		`"replyTo":{"email":"support@example.com","name":"Support"}`,
		`"attachment":[{"name":"a.txt","content":"ZGF0YQ=="}]`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request body = %s, want it to contain %s", body, want)
		}
	}
}

func TestBrevoSender_Send_ReturnsErrorsWithoutLeakingDetails(t *testing.T) {
	t.Run("transport failure", func(t *testing.T) {
		s, err := newBrevoSender("app@example.com", BrevoOptions{APIKey: "k"},
			&http.Client{Transport: failingTransport{}})
		if err != nil {
			t.Fatalf("newBrevoSender() error = %v", err)
		}

		err = s.Send(context.Background(), validMessage())

		if err == nil {
			t.Fatal("Send() error = nil, want a transport error")
		}

		if want := "mailer: brevo request: connection refused"; err.Error() != want {
			t.Errorf("Send() error = %q, want %q (no request URL)", err, want)
		}
	})

	t.Run("api error with a huge body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(bytes.Repeat([]byte("x"), 100_000))
		}))
		t.Cleanup(srv.Close)

		s, err := newBrevoSender("app@example.com", BrevoOptions{APIKey: "k"}, srv.Client())
		if err != nil {
			t.Fatalf("newBrevoSender() error = %v", err)
		}

		s.endpoint = srv.URL

		err = s.Send(context.Background(), validMessage())

		if err == nil {
			t.Fatal("Send() error = nil, want an API error")
		}

		if !strings.Contains(err.Error(), "status 500") {
			t.Errorf("Send() error = %.80q..., want it to mention status 500", err)
		}

		if len(err.Error()) > 5000 {
			t.Errorf("error length = %d, want the API body capped at 4 KiB", len(err.Error()))
		}
	})

	t.Run("invalid message", func(t *testing.T) {
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
		}))
		t.Cleanup(srv.Close)

		s, err := newBrevoSender("app@example.com", BrevoOptions{APIKey: "k"}, srv.Client())
		if err != nil {
			t.Fatalf("newBrevoSender() error = %v", err)
		}

		s.endpoint = srv.URL
		m := validMessage()
		m.To, m.Bcc = nil, nil

		if err := s.Send(context.Background(), m); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("Send() error = %v, want ErrInvalidMessage", err)
		}

		if called {
			t.Error("API was called, want no request for an invalid message")
		}
	})
}

func TestLogSender_Send_LogsMessageInsteadOfSending(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	s := newLogSender("", zap.New(core))

	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}

	fields := entries[0].ContextMap()
	if got := fields["from"]; got != `"dev" <dev@localhost>` {
		t.Errorf("from = %v, want the default dev sender", got)
	}

	if got := fields["body"]; got != "Hi" {
		t.Errorf("body = %v, want the text body %q", got, "Hi")
	}

	if got := fmt.Sprint(fields["to"]); got != "[Bob <bob@example.com> audit@example.com]" {
		t.Errorf("to = %s, want To and Bcc recipients", got)
	}
}

func TestNewLogSender_NilLoggerIsSafe(t *testing.T) {
	s := newLogSender("app@example.com", nil)

	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("Send() with a nil logger error = %v, want nil", err)
	}
}
