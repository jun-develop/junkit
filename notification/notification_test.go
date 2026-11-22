package notification_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: notification_test.go
// Purpose: Verify the Telegram notifier: message banners, error reports, secret masking and queue limits.
// Type: Unit (Telegram is replaced by fakeTelegramClient)
//
// Test functions: 7
// Total scenarios: 8
//
// #  Test Case                                              Scenario                                                      Expected Result
// 1  TestTelegramNotifier_Send_AddsLevelBanner              Error level                                                   One message with the "Error" banner and the text
// 2  TestTelegramNotifier_Send_AddsLevelBanner              Warning level                                                 One message with the "Warning" banner and the text
// 3  TestTelegramNotifier_DisabledSendsNothing              Notifier disabled; Send and ReportError                       No message sent
// 4  TestTelegramNotifier_ReportError_IncludesErrorDetails  500 error report for a POST request                           Message has the error, function, curl command and banner
// 5  TestTelegramNotifier_ReportError_KeepsCredentialsOut   Request with secrets in header, query, body; HTML trace ID    Secrets absent, Authorization masked, trace ID escaped
// 6  TestTelegramNotifier_ReportError_SendsStackAsDocument  5000-char error with a stack trace                            One message under 4096 runes; stack sent as a document without caption
// 7  TestTelegramNotifier_ReportError_ClientErrorBanner     400 error report                                              Message has the "Client Error" banner
// 8  TestTelegramNotifier_FullQueueDropsAlerts              150 alerts while the client is blocked, then one after Close  About 100 kept, the rest and the late one counted as dropped
//
// =============================================================================

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jun-develop/junkit/logger"
	"github.com/jun-develop/junkit/notification"
)

// document is one SendDocument call recorded by fakeTelegramClient.
type document struct {
	filename string
	caption  string
	content  string
}

// fakeTelegramClient is a telegram.Client that records every call instead of contacting Telegram.
// A non-nil block makes SendMessage wait until it is closed.
type fakeTelegramClient struct {
	block chan struct{}

	mu        sync.Mutex
	messages  []string
	documents []document
}

func (f *fakeTelegramClient) SendMessage(_ context.Context, _ string, message string) error {
	if f.block != nil {
		<-f.block
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, message)

	return nil
}

func (f *fakeTelegramClient) SendDocument(_ context.Context, _ string, content io.Reader, filename string, caption string) error {
	data, _ := io.ReadAll(content)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.documents = append(f.documents, document{filename, caption, string(data)})

	return nil
}

// newNotifier returns an enabled notifier and its fake client; flush waits for queued alerts.
func newNotifier(t *testing.T, enabled bool) (*notification.TelegramNotifier, *fakeTelegramClient, func()) {
	t.Helper()
	client := &fakeTelegramClient{}
	n := notification.NewNotifier(notification.Options{TelegramChatID: "123", Enabled: enabled}, client, nil)
	flush := func() {
		t.Helper()
		if err := n.Close(context.Background()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}

	return n, client, flush
}

func TestTelegramNotifier_Send_AddsLevelBanner(t *testing.T) {
	tests := []struct {
		name       string
		level      notification.Level
		wantBanner string
	}{
		{"error level", notification.LevelError, "Error"},
		{"warning level", notification.LevelWarning, "Warning"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, client, flush := newNotifier(t, true)

			n.Send(context.Background(), tt.level, "something happened")
			flush()

			if len(client.messages) != 1 {
				t.Fatalf("sent %d messages, want 1: %q", len(client.messages), client.messages)
			}

			if !strings.Contains(client.messages[0], tt.wantBanner) {
				t.Errorf("message = %q, want the %s banner", client.messages[0], tt.wantBanner)
			}

			if !strings.Contains(client.messages[0], "something happened") {
				t.Errorf("message = %q, want the text %q", client.messages[0], "something happened")
			}
		})
	}
}

func TestTelegramNotifier_DisabledSendsNothing(t *testing.T) {
	n, client, flush := newNotifier(t, false)
	n.Send(context.Background(), notification.LevelError, "x")
	n.ReportError(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), notification.ErrorReport{Error: "boom"})
	flush()

	if len(client.messages) != 0 {
		t.Fatalf("got %d messages, want 0 when disabled", len(client.messages))
	}
}

func TestTelegramNotifier_ReportError_IncludesErrorDetails(t *testing.T) {
	n, client, flush := newNotifier(t, true)
	r := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"email":"jane@example.com"}`))
	r.Header.Set("User-Agent", "test-agent")

	n.ReportError(r.Context(), r, notification.ErrorReport{
		Error: "database unreachable", Location: "/app/repo.go", Function: "GetUser", Line: 42,
		StatusCode: http.StatusInternalServerError,
	})
	flush()

	if len(client.messages) != 1 {
		t.Fatalf("sent %d messages, want 1", len(client.messages))
	}

	msg := client.messages[0]
	for _, want := range []string{"database unreachable", "GetUser", "curl -X POST", "Error Notification"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message = %q, want %q included", msg, want)
		}
	}
}

func TestTelegramNotifier_ReportError_KeepsCredentialsOut(t *testing.T) {
	n, client, flush := newNotifier(t, true)
	r := httptest.NewRequest(http.MethodPost, "/login?token=query-secret", strings.NewReader(`{"password":"body-secret"}`))
	r.Header.Set("Authorization", "Bearer bearer-secret")
	r.Header.Set("X-Internal-Secret", "header-secret")
	r.Header.Set("X-Forwarded-Proto", "javascript")
	ctx := logger.WithTraceID(r.Context(), `</pre><a href="https://example.invalid">x</a>`)

	n.ReportError(ctx, r, notification.ErrorReport{Error: "boom", StatusCode: http.StatusInternalServerError})
	flush()

	if len(client.messages) != 1 {
		t.Fatalf("sent %d messages, want 1", len(client.messages))
	}

	msg := client.messages[0]
	for _, secret := range []string{"bearer-secret", "header-secret", "query-secret", "body-secret", "javascript:"} {
		if strings.Contains(msg, secret) {
			t.Errorf("message contains %q, want it masked or left out:\n%s", secret, msg)
		}
	}

	if !strings.Contains(msg, "Authorization: Bearer ****") {
		t.Errorf("message = %q, want the masked Authorization header kept for debugging", msg)
	}

	if strings.Contains(msg, `<a href=`) {
		t.Errorf("message = %q, want the trace ID HTML-escaped", msg)
	}
}

func TestTelegramNotifier_ReportError_SendsStackAsDocument(t *testing.T) {
	n, client, flush := newNotifier(t, true)
	n.ReportError(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil), notification.ErrorReport{
		Error:      strings.Repeat("e", 5000),
		Stack:      "goroutine 1 [running]:\nmain.main()",
		StatusCode: http.StatusInternalServerError,
	})
	flush()

	if len(client.messages) != 1 {
		t.Fatalf("sent %d messages, want 1", len(client.messages))
	}

	if runes := len([]rune(client.messages[0])); runes > 4096 {
		t.Errorf("message length = %d runes, want <= 4096 (Telegram's limit)", runes)
	}

	if len(client.documents) != 1 {
		t.Fatalf("sent %d documents, want 1: %+v", len(client.documents), client.documents)
	}

	if client.documents[0].caption != "" {
		t.Errorf("document caption = %q, want empty", client.documents[0].caption)
	}

	if !strings.Contains(client.documents[0].content, "goroutine 1") {
		t.Errorf("document content = %q, want the stack trace", client.documents[0].content)
	}
}

func TestTelegramNotifier_ReportError_ClientErrorBanner(t *testing.T) {
	n, client, flush := newNotifier(t, true)
	n.ReportError(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil),
		notification.ErrorReport{Error: "bad input", StatusCode: http.StatusBadRequest})
	flush()

	if len(client.messages) != 1 {
		t.Fatalf("sent %d messages, want 1", len(client.messages))
	}

	if !strings.Contains(client.messages[0], "Client Error") {
		t.Fatalf("message = %q, want the client-error banner for a 4xx status", client.messages[0])
	}
}

func TestTelegramNotifier_FullQueueDropsAlerts(t *testing.T) {
	client := &fakeTelegramClient{block: make(chan struct{})}
	n := notification.NewNotifier(notification.Options{TelegramChatID: "123", Enabled: true}, client, nil)

	for range 150 {
		n.Send(context.Background(), notification.LevelWarning, "x")
	}

	close(client.block)
	if err := n.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// The worker may hold one alert while blocked, so 100 or 101 are kept.
	sent, dropped := len(client.messages), n.Dropped()
	if sent+int(dropped) != 150 {
		t.Fatalf("sent %d + dropped %d = %d, want every one of the 150 alerts counted", sent, dropped, sent+int(dropped))
	}

	if dropped < 49 {
		t.Fatalf("dropped %d, want >= 49 (queue capped at about 100)", dropped)
	}

	n.Send(context.Background(), notification.LevelWarning, "after close")
	if n.Dropped() != 151-int64(len(client.messages)) {
		t.Fatalf("Dropped() = %d, want a send after Close counted as dropped", n.Dropped())
	}
}
