package notification_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/notification"
)

// fakeTelegramClient is a telegram.Client that records every call instead
// of contacting Telegram.
type fakeTelegramClient struct {
	mu       sync.Mutex
	messages []string

	documents []struct {
		filename string
		caption  string
		content  string
	}
}

func (f *fakeTelegramClient) SendMessage(_ context.Context, _ string, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, message)

	return nil
}

func (f *fakeTelegramClient) SendDocument(_ context.Context, _ string, content io.Reader, filename string, caption string) error {
	data, _ := io.ReadAll(content)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.documents = append(f.documents, struct {
		filename string
		caption  string
		content  string
	}{filename, caption, string(data)})

	return nil
}

func (f *fakeTelegramClient) lastMessage() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		return ""
	}

	return f.messages[len(f.messages)-1]
}

func newGinContext(method, target, body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))

	return c
}

func TestSendMessageHTML(t *testing.T) {
	t.Run("error type gets the error banner", func(t *testing.T) {
		client := &fakeTelegramClient{}
		n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123"})

		n.SendMessageHTML(newGinContext(http.MethodGet, "/", ""), "something broke", "error")

		if !strings.Contains(client.lastMessage(), "Error") {
			t.Fatalf("message = %q, want the error banner", client.lastMessage())
		}

		if !strings.Contains(client.lastMessage(), "something broke") {
			t.Fatalf("message = %q, want the original text included", client.lastMessage())
		}
	})

	t.Run("any other type gets the warning banner", func(t *testing.T) {
		client := &fakeTelegramClient{}
		n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123"})

		n.SendMessageHTML(newGinContext(http.MethodGet, "/", ""), "heads up", "warning")

		if !strings.Contains(client.lastMessage(), "Warning") {
			t.Fatalf("message = %q, want the warning banner", client.lastMessage())
		}
	})
}

func TestSendErrorNotification_SkippedOutsideProduction(t *testing.T) {
	client := &fakeTelegramClient{}
	n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123", Environment: "development"})

	n.SendErrorNotification(newGinContext(http.MethodGet, "/", ""), map[string]any{"error": "boom"}, http.StatusInternalServerError)

	if len(client.messages) != 0 {
		t.Fatalf("got %d messages, want 0 outside production", len(client.messages))
	}
}

func TestSendErrorNotification_SentInProduction(t *testing.T) {
	client := &fakeTelegramClient{}
	n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123", Environment: "production"})

	c := newGinContext(http.MethodPost, "/users", `{"email":"jane@example.com"}`)
	c.Request.Header.Set("User-Agent", "test-agent")

	n.SendErrorNotification(c, map[string]any{
		"error":    "database unreachable",
		"location": "/app/repo.go",
		"function": "GetUser",
		"line":     42,
	}, http.StatusInternalServerError)

	msg := client.lastMessage()
	if msg == "" {
		t.Fatal("expected a message to be sent in production")
	}

	if !strings.Contains(msg, "database unreachable") {
		t.Errorf("message = %q, want the error text included", msg)
	}

	if !strings.Contains(msg, "GetUser") {
		t.Errorf("message = %q, want the origin function included", msg)
	}

	if !strings.Contains(msg, "curl -X POST") {
		t.Errorf("message = %q, want a reproducible curl command included", msg)
	}
}

func TestSendErrorNotification_KeepsCredentialsOutOfTheAlert(t *testing.T) {
	client := &fakeTelegramClient{}
	n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123", Environment: "production"})

	c := newGinContext(http.MethodPost, "/login?token=query-secret", `{"password":"body-secret"}`)
	c.Request.Header.Set("Authorization", "Bearer bearer-secret")
	c.Request.Header.Set("X-Internal-Secret", "header-secret")
	c.Set("trace_id", `</pre><a href="https://example.invalid">x</a>`)

	n.SendErrorNotification(c, map[string]any{"error": "boom"}, http.StatusInternalServerError)

	msg := client.lastMessage()
	for _, secret := range []string{"bearer-secret", "header-secret", "query-secret", "body-secret"} {
		if strings.Contains(msg, secret) {
			t.Errorf("message contains %q, want it masked or left out:\n%s", secret, msg)
		}
	}

	if !strings.Contains(msg, `"Authorization: Bearer ****"`) {
		t.Errorf("message = %q, want the masked Authorization header kept for debugging", msg)
	}

	if strings.Contains(msg, `<a href=`) {
		t.Errorf("message = %q, want the trace ID HTML-escaped", msg)
	}
}

func TestSendErrorNotification_WithStackTraceSendsDocument(t *testing.T) {
	client := &fakeTelegramClient{}
	n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123", Environment: "production"})

	n.SendErrorNotification(newGinContext(http.MethodGet, "/", ""), map[string]any{
		"error": "panic!",
		"stack": "goroutine 1 [running]:\nmain.main()",
	}, http.StatusInternalServerError)

	if len(client.documents) != 1 {
		t.Fatalf("got %d documents sent, want 1 when a stack trace is present", len(client.documents))
	}

	if !strings.Contains(client.documents[0].content, "goroutine 1") {
		t.Errorf("document content = %q, want the stack trace included", client.documents[0].content)
	}

	if len(client.messages) != 0 {
		t.Error("SendMessage should not be called directly when a stack trace document is sent instead")
	}
}

func TestSendErrorNotification_SQLIsIncluded(t *testing.T) {
	client := &fakeTelegramClient{}
	n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123", Environment: "production"})

	n.SendErrorNotification(newGinContext(http.MethodGet, "/", ""), map[string]any{"error": "boom"}, http.StatusInternalServerError, "SELECT * FROM users")

	if !strings.Contains(client.lastMessage(), "SELECT * FROM users") {
		t.Fatalf("message = %q, want the SQL included", client.lastMessage())
	}
}

func TestSendErrorNotification_ClientErrorGetsDifferentBanner(t *testing.T) {
	client := &fakeTelegramClient{}
	n := notification.NewNotifier(client, notification.Options{TelegramChatID: "123", Environment: "production"})

	n.SendErrorNotification(newGinContext(http.MethodGet, "/", ""), map[string]any{"error": "bad input"}, http.StatusBadRequest)

	if !strings.Contains(client.lastMessage(), "Client Error") {
		t.Fatalf("message = %q, want the client-error banner for a 4xx status", client.lastMessage())
	}
}
