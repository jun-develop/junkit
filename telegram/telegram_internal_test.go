package telegram

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: telegram_internal_test.go
// Purpose: Verify the Telegram Bot API client: requests sent, silent skips, error handling and defaults.
// Type: Unit (the Bot API is an httptest server)
//
// Test functions: 9
// Total scenarios: 11
//
// #   Test Case                                                Scenario                               Expected Result
// 1   TestClient_SendMessage_PostsToSendMessageEndpoint        Valid token and chat ID                Request goes to /bot<token>/sendMessage
// 2   TestClient_SendMessage_SkipsWithoutToken                 Empty bot token                        Returns nil; no HTTP request
// 3   TestClient_SkipsWithoutChatID                            SendMessage with an empty chat ID      Returns nil; no HTTP request
// 4   TestClient_SkipsWithoutChatID                            SendDocument with an empty chat ID     Returns nil; no HTTP request
// 5   TestClient_SendMessage_ReturnsAPIErrorBody               API answers 403 with a description     Error includes the API description
// 6   TestClient_SendDocument_UploadsFileWithCaption           Document with filename and caption     Multipart request to sendDocument with that filename and caption
// 7   TestClient_SendDocument_SkipsWithNilContent              nil content                            Returns nil; no HTTP request
// 8   TestNewClient_NilHTTPClientDefaults                      nil HTTP client                        Non-nil default client; prefix points at api.telegram.org
// 9   TestClient_SendDocument_BuildRequestErrorReleasesWriter  Invalid API URL                        Returns an error; writer goroutine exits within 1s
// 10  TestClient_TransportErrorHidesToken                      SendMessage over a failing transport   "telegram: ..." error without the token
// 11  TestClient_TransportErrorHidesToken                      SendDocument over a failing transport  "telegram: ..." error without the token
//
// =============================================================================

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// newTestClient returns a client whose botPrefix points at a local httptest.Server running handler.
func newTestClient(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	mux := http.NewServeMux()
	if handler != nil {
		mux.HandleFunc("/", handler)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &client{
		httpClient: server.Client(),
		botPrefix:  server.URL + "/bottest-token",
		token:      "test-token",
	}
}

// failingTransport fails every request.
type failingTransport struct{}

// RoundTrip implements http.RoundTripper.
func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

func TestClient_SendMessage_PostsToSendMessageEndpoint(t *testing.T) {
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	if err := c.SendMessage(context.Background(), "123", "hello"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	if gotPath != "/bottest-token/sendMessage" {
		t.Fatalf("request path = %q, want the sendMessage endpoint", gotPath)
	}
}

func TestClient_SendMessage_SkipsWithoutToken(t *testing.T) {
	called := false
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	c.token = ""

	if err := c.SendMessage(context.Background(), "123", "hello"); err != nil {
		t.Fatalf("SendMessage() error = %v, want nil (silent skip)", err)
	}

	if called {
		t.Error("no HTTP request should be made when the token is missing")
	}
}

func TestClient_SkipsWithoutChatID(t *testing.T) {
	tests := []struct {
		name string
		send func(c *client) error
	}{
		{"SendMessage", func(c *client) error {
			return c.SendMessage(context.Background(), "", "hello")
		}},
		{"SendDocument", func(c *client) error {
			return c.SendDocument(context.Background(), "", strings.NewReader("content"), "a.txt", "caption")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})

			if err := tt.send(c); err != nil {
				t.Fatalf("%s() with empty chat ID error = %v, want nil (silent skip)", tt.name, err)
			}

			if called {
				t.Errorf("%s() with empty chat ID made an HTTP request, want none", tt.name)
			}
		})
	}
}

func TestClient_SendMessage_ReturnsAPIErrorBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"description": "bot was blocked by the user"}`))
	})

	err := c.SendMessage(context.Background(), "123", "hello")
	if err == nil {
		t.Fatal("SendMessage() error = nil, want an error for a non-200 response")
	}

	if !strings.Contains(err.Error(), "bot was blocked") {
		t.Fatalf("error = %v, want the API's error body included", err)
	}
}

func TestClient_SendDocument_UploadsFileWithCaption(t *testing.T) {
	var gotPath, gotFilename, gotCaption string
	var parseErr error
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if parseErr = r.ParseMultipartForm(1 << 20); parseErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		gotCaption = r.FormValue("caption")
		if fh := r.MultipartForm.File["document"]; len(fh) == 1 {
			gotFilename = fh[0].Filename
		}

		w.WriteHeader(http.StatusOK)
	})

	err := c.SendDocument(context.Background(), "123", strings.NewReader("stack trace content"), "stack.txt", "a caption")
	if parseErr != nil {
		t.Fatalf("server could not parse the multipart form: %v", parseErr)
	}

	if err != nil {
		t.Fatalf("SendDocument() error = %v", err)
	}

	if gotPath != "/bottest-token/sendDocument" {
		t.Fatalf("request path = %q, want the sendDocument endpoint", gotPath)
	}

	if gotFilename != "stack.txt" {
		t.Fatalf("uploaded filename = %q, want %q", gotFilename, "stack.txt")
	}

	if gotCaption != "a caption" {
		t.Fatalf("caption = %q, want %q", gotCaption, "a caption")
	}
}

func TestClient_SendDocument_SkipsWithNilContent(t *testing.T) {
	called := false
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	if err := c.SendDocument(context.Background(), "123", nil, "f.txt", ""); err != nil {
		t.Fatalf("SendDocument() error = %v, want nil (silent skip)", err)
	}

	if called {
		t.Error("no HTTP request should be made when content is nil")
	}
}

func TestNewClient_NilHTTPClientDefaults(t *testing.T) {
	c := NewClient(Options{Token: "abc"}, nil)
	impl, ok := c.(*client)
	if !ok {
		t.Fatalf("NewClient() returned %T, want *client", c)
	}

	if impl.httpClient == nil {
		t.Fatal("httpClient should default to a non-nil client when nil is passed")
	}

	if impl.botPrefix != "https://api.telegram.org/botabc" {
		t.Fatalf("botPrefix = %q, want the real Telegram API host", impl.botPrefix)
	}
}

func TestClient_SendDocument_BuildRequestErrorReleasesWriter(t *testing.T) {
	c := newTestClient(t, nil)
	c.botPrefix = "://bad-url"
	before := runtime.NumGoroutine()

	if err := c.SendDocument(context.Background(), "123", strings.NewReader("doc"), "a.txt", ""); err == nil {
		t.Fatal("SendDocument() error = nil, want the request build error")
	}

	for deadline := time.Now().Add(time.Second); runtime.NumGoroutine() > before; {
		if time.Now().After(deadline) {
			t.Fatal("the multipart writer goroutine is still blocked after SendDocument returned")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestClient_TransportErrorHidesToken(t *testing.T) {
	c := NewClient(Options{Token: "secret-bot-token"}, &http.Client{Transport: failingTransport{}})

	tests := []struct {
		name string
		send func() error
	}{
		{"send message", func() error { return c.SendMessage(context.Background(), "123", "hi") }},
		{"send document", func() error {
			return c.SendDocument(context.Background(), "123", strings.NewReader("doc"), "a.txt", "")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.send()
			if err == nil {
				t.Fatal("error = nil, want the transport error")
			}

			if strings.Contains(err.Error(), "secret-bot-token") {
				t.Errorf("error = %q, want it without the bot token", err)
			}

			if !strings.HasPrefix(err.Error(), "telegram: ") {
				t.Errorf("error = %q, want the \"telegram: \" prefix", err)
			}
		})
	}
}
