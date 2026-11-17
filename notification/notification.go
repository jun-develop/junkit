// Package notification sends operational alerts to Telegram through a bounded background queue, so
// a slow or failing Telegram never delays a request.
package notification

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/security"
	"github.com/jun-develop/junkit/telegram"
)

// queueSize bounds the alerts waiting to be sent; more are dropped.
const queueSize = 100

// sendTimeout bounds each Telegram call made by the queue worker.
const sendTimeout = 10 * time.Second

// Telegram limits a message to 4096 characters; the report fields are cut to stay under it.
const (
	maxErrorLen = 2000
	maxCurlLen  = 1200
	maxFieldLen = 200
)

// Level is an alert's severity.
type Level int

// Alert levels.
const (
	LevelWarning Level = iota
	LevelError
)

// Notifier sends operational alerts. Both methods only queue the alert and return at once; sends
// are best-effort, and a full queue drops the alert.
type Notifier interface {
	// Send queues msg, Telegram HTML under 4096 characters with request values escaped by the
	// caller, under a banner for level.
	Send(ctx context.Context, level Level, msg string)

	// ReportError queues a report of a failed request r, with a cURL repro whose secret headers
	// are masked and whose query and body are left out. ctx supplies the trace ID.
	ReportError(ctx context.Context, r *http.Request, report ErrorReport)
}

// ErrorReport describes a failed request for ReportError.
type ErrorReport struct {
	// Error is the error or panic value as text.
	Error string

	// Location, Line and Function are the error's origin in the source.
	Location string
	Line     int
	Function string

	// Stack, when set, is sent as a document after the message.
	Stack string

	// StatusCode is the response status; 5xx gets the error banner, anything else the client-error one.
	StatusCode int

	// ClientIP is the client address; r.RemoteAddr is used when empty.
	ClientIP string
}

// Options configures the notification service.
type Options struct {
	// TelegramChatID is the chat every alert is sent to.
	TelegramChatID string

	// Enabled turns alerts on; when false, Send and ReportError do nothing.
	Enabled bool
}

// TelegramNotifier is the Telegram Notifier. A single worker sends queued alerts in order; Close
// stops it.
type TelegramNotifier struct {
	opts     Options
	tgClient telegram.Client
	log      *zap.Logger

	queue   chan func(context.Context) error
	mu      sync.RWMutex
	closed  bool
	done    chan struct{}
	dropped atomic.Int64
}

// NewNotifier creates a TelegramNotifier sending through tgClient and starts its worker; call
// Close when done. Failed sends are logged to log (nil discards them), since no caller is left to
// return them to.
//
// Usage:
//
//	n := notification.NewNotifier(notification.Options{TelegramChatID: chatID, Enabled: true}, tgClient, log)
//	defer n.Close(ctx)
func NewNotifier(opts Options, tgClient telegram.Client, log *zap.Logger) *TelegramNotifier {
	if log == nil {
		log = zap.NewNop()
	}

	n := &TelegramNotifier{
		opts:     opts,
		tgClient: tgClient,
		log:      log,
		queue:    make(chan func(context.Context) error, queueSize),
		done:     make(chan struct{}),
	}

	go n.run()

	return n
}

// run sends queued alerts until Close, each with its own timeout.
func (n *TelegramNotifier) run() {
	defer close(n.done)

	for send := range n.queue {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		if err := send(ctx); err != nil {
			n.log.Error("notification: send failed", zap.Error(err))
		}

		cancel()
	}
}

// enqueue queues send, or drops it when the queue is full or closed.
func (n *TelegramNotifier) enqueue(send func(context.Context) error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		n.dropped.Add(1)
		return
	}

	select {
	case n.queue <- send:
	default:
		n.dropped.Add(1)
	}
}

// Dropped returns how many alerts were dropped because the queue was full or closed.
func (n *TelegramNotifier) Dropped() int64 {
	return n.dropped.Load()
}

// Close stops accepting alerts and waits until the queued ones are sent or ctx ends.
func (n *TelegramNotifier) Close(ctx context.Context) error {
	n.mu.Lock()
	if !n.closed {
		n.closed = true
		close(n.queue)
	}

	n.mu.Unlock()

	select {
	case <-n.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("notification: close: %w", ctx.Err())
	}
}

// Send implements Notifier.
func (n *TelegramNotifier) Send(_ context.Context, level Level, msg string) {
	if !n.opts.Enabled {
		return
	}

	text := banner(level) + msg
	n.enqueue(func(ctx context.Context) error {
		return n.tgClient.SendMessage(ctx, n.opts.TelegramChatID, text)
	})
}

// ReportError implements Notifier. The request is rendered here, before queueing, since r is not
// safe to read once the handler returns.
func (n *TelegramNotifier) ReportError(ctx context.Context, r *http.Request, report ErrorReport) {
	if !n.opts.Enabled {
		return
	}

	text := buildErrorNotification(ctx, r, report)
	stack := report.Stack
	at := time.Now()

	n.enqueue(func(ctx context.Context) error {
		err := n.tgClient.SendMessage(ctx, n.opts.TelegramChatID, text)
		if stack == "" {
			return err
		}

		content := fmt.Sprintf("STACK TRACE\nGenerated: %s\n----------------------------------------\n\n%s",
			at.Format("2006-01-02 15:04:05 MST"), stack)
		filename := fmt.Sprintf("stack-%s.txt", at.Format("20060102-150405"))

		return errors.Join(err, n.tgClient.SendDocument(ctx, n.opts.TelegramChatID,
			strings.NewReader(content), filename, ""))
	})
}

// banner returns the heading for level.
func banner(level Level) string {
	if level == LevelError {
		return "🚨 <b>Error</b> 🚨\n\n"
	}

	return "🟠 <b>Warning</b> 🟠\n\n"
}

// buildErrorNotification renders the HTML error message body, cutting long fields so it stays
// under Telegram's message limit.
func buildErrorNotification(ctx context.Context, r *http.Request, report ErrorReport) string {
	var sb strings.Builder

	if report.StatusCode >= 500 {
		sb.WriteString("🚨 <b>Error Notification</b> 🚨\n\n")
	} else {
		sb.WriteString("🟠 <b>Client Error</b> 🟠\n\n")
	}

	ip := report.ClientIP
	if ip == "" {
		ip = r.RemoteAddr
	}

	traceID := logger.GetTraceID(ctx)
	if traceID == "" {
		traceID = "-"
	}

	fmt.Fprintf(&sb,
		"🛜 <b>User Agent:</b> %s\n"+
			"💻 <b>IP:</b> %s\n"+
			"📡 <b>Status:</b> %d\n"+
			"📦 <b>Method:</b> %s\n\n",
		escape(orDash(r.UserAgent()), maxFieldLen),
		escape(ip, maxFieldLen),
		report.StatusCode,
		escape(r.Method, maxFieldLen),
	)

	fmt.Fprintf(&sb,
		"📝 <b>Location:</b>\n<pre>"+
			"File: %s\n"+
			"Function: %s\n"+
			"Line: %d\n"+
			"Trace ID: %s"+
			"</pre>\n\n",
		escape(orDash(report.Location), maxFieldLen),
		escape(orDash(report.Function), maxFieldLen),
		report.Line,
		escape(traceID, maxFieldLen),
	)

	fmt.Fprintf(&sb, "🐞 <b>Error:</b>\n<pre>%s</pre>\n\n", escape(orDash(report.Error), maxErrorLen))
	fmt.Fprintf(&sb, "🍺 <b>cURL Command:</b>\n<pre>%s</pre>\n", escape(generateCurlCommand(r), maxCurlLen))

	return sb.String()
}

// orDash returns s, or "-" when it is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}

// escape cuts s to max runes and HTML-escapes it for Telegram.
func escape(s string, max int) string {
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "…"
	}

	return html.EscapeString(s)
}

// skipCurlHeaders are headers left out of the cURL repro: noise, or a secret even when masked.
var skipCurlHeaders = map[string]bool{
	"content-length": true,
	"user-agent":     true,
	"postman-token":  true,
	"cookie":         true,
}

// generateCurlCommand renders a cURL command for a request. Secret header values are masked, and
// the query string and body are left out: the alert is stored in a chat, where credentials they may
// carry must not end up.
func generateCurlCommand(r *http.Request) string {
	parts := []string{fmt.Sprintf("curl -X %s %q", r.Method, getFullRequestURL(r))}

	for k, values := range r.Header {
		if skipCurlHeaders[strings.ToLower(k)] {
			continue
		}

		for _, v := range values {
			parts = append(parts, fmt.Sprintf("-H %q", k+": "+security.MaskHeader(k, v)))
		}
	}

	return strings.Join(parts, " \\\n  ")
}

// getFullRequestURL reconstructs the request's absolute URL without its query string. Only an
// X-Forwarded-Proto of "https" is honored, so a client cannot inject an arbitrary scheme.
func getFullRequestURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.EscapedPath())
}
