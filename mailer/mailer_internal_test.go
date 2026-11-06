package mailer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

func validMessage() Message {
	return Message{
		To:      []string{"Bob <bob@example.com>"},
		Bcc:     []string{"audit@example.com"},
		Subject: "Hello Junkit",
		HTML:    "<p>Hi</p>",
		Text:    "Hi",
	}
}

func TestPrepare_Validation(t *testing.T) {
	cases := map[string]func(*Message){
		"no recipients":     func(m *Message) { m.To, m.Bcc = nil, nil },
		"bad address":       func(m *Message) { m.To = []string{"not-an-email"} },
		"header injection":  func(m *Message) { m.Subject = "hi\r\nBcc: evil@example.com" },
		"empty body":        func(m *Message) { m.HTML, m.Text = "", "" },
		"bad attachment":    func(m *Message) { m.Attachments = []Attachment{{Filename: ""}} },
		"bad from override": func(m *Message) { m.From = "nope" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := validMessage()
			mutate(&m)
			if _, err := prepare(m, "App <app@example.com>"); !errors.Is(err, ErrInvalidMessage) {
				t.Fatalf("prepare() error = %v, want ErrInvalidMessage", err)
			}
		})
	}

	if _, err := prepare(validMessage(), "App <app@example.com>"); err != nil {
		t.Fatalf("prepare(valid) error = %v", err)
	}
}

func TestBuildMIME_AlternativeWithAttachment(t *testing.T) {
	m := validMessage()
	m.Attachments = []Attachment{{Filename: "báo cáo.txt", ContentType: "text/plain", Data: []byte("data")}}
	env, err := prepare(m, "App <app@example.com>")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := buildMIME(env, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if parsed.Header.Get("Bcc") != "" {
		t.Fatal("Bcc must not appear in headers")
	}

	subject, _ := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if subject != "Xin chào" {
		t.Fatalf("subject = %q", subject)
	}

	mt, params, _ := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if mt != "multipart/mixed" {
		t.Fatalf("content-type = %q", mt)
	}

	mr := multipart.NewReader(parsed.Body, params["boundary"])
	body, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}

	bt, bparams, _ := mime.ParseMediaType(body.Header.Get("Content-Type"))
	if bt != "multipart/alternative" {
		t.Fatalf("body content-type = %q", bt)
	}

	alt := multipart.NewReader(body, bparams["boundary"])
	var texts []string
	for {
		p, err := alt.NextPart()
		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		b, _ := io.ReadAll(p) // multipart.Reader decodes quoted-printable
		texts = append(texts, string(b))
	}

	if len(texts) != 2 || texts[0] != "Hi" || texts[1] != "<p>Hi</p>" {
		t.Fatalf("alternatives = %q", texts)
	}

	att, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}

	if att.FileName() != "báo cáo.txt" {
		t.Fatalf("attachment filename = %q", att.FileName())
	}
}

func TestBrevoSender_Send(t *testing.T) {
	var got brevoRequest
	var apiKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey = r.Header.Get("api-key")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	s, err := newBrevoSender("App <app@example.com>", BrevoOptions{APIKey: "k"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	s.endpoint = srv.URL
	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if apiKey != "k" || got.Sender.Email != "app@example.com" || got.To[0].Name != "Bob" ||
		got.Bcc[0].Email != "audit@example.com" || got.HTMLContent != "<p>Hi</p>" {
		t.Fatalf("unexpected request: key=%q body=%+v", apiKey, got)
	}
}

func TestBrevoSender_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"unauthorized"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	s, _ := newBrevoSender("app@example.com", BrevoOptions{APIKey: "k"}, srv.Client())
	s.endpoint = srv.URL
	if err := s.Send(context.Background(), validMessage()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Send() error = %v, want status 401", err)
	}
}

// fakeSMTP is a minimal SMTP server (no TLS, no auth) recording one transaction.
func fakeSMTP(t *testing.T) (port int, result <-chan []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
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
		var log []string

		reply("220 fake")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimRight(line, "\r\n")
			log = append(log, line)
			switch cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0]); cmd {
			case "EHLO", "HELO":
				reply("250 fake")
			case "DATA":
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

				reply("250 queued")
			case "QUIT":
				reply("221 bye")
				ch <- log
				return
			default:
				reply("250 ok")
			}
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, ch
}

func TestSMTPSender_Send(t *testing.T) {
	port, result := fakeSMTP(t)
	s, err := newSMTPSender("App <app@example.com>", SMTPOptions{Host: "127.0.0.1", Port: port, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	log := strings.Join(<-result, "\n")
	for _, want := range []string{"MAIL FROM:<app@example.com>", "RCPT TO:<bob@example.com>", "RCPT TO:<audit@example.com>", "Subject: =?utf-8?q?Xin_ch=C3=A0o?="} {
		if !strings.Contains(log, want) {
			t.Fatalf("transcript missing %q:\n%s", want, log)
		}
	}
}

func TestNew(t *testing.T) {
	logger := zaptest.NewLogger(t)
	if _, err := New(Options{Driver: "sendgridd", From: "a@example.com"}, nil, logger); err == nil {
		t.Fatal("unknown driver must fail")
	}

	if _, err := New(Options{Driver: DriverSMTP, From: "bad"}, nil, logger); err == nil {
		t.Fatal("invalid From must fail")
	}

	if _, err := New(Options{Driver: DriverBrevo, From: "a@example.com"}, nil, logger); err == nil {
		t.Fatal("missing brevo api key must fail")
	}

	s, err := New(Options{}, nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Send(context.Background(), validMessage()); err != nil {
		t.Fatalf("log driver Send() error = %v", err)
	}

}
