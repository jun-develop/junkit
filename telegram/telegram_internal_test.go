package telegram

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

// newTestClient returns a client whose botPrefix points at a local httptest.Server.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*client, *http.ServeMux) {
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
	}, mux
}

func TestClient_SendMessage_Success(t *testing.T) {
	var gotPath string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestClient_SendMessage_SkipsWithoutTokenOrChatID(t *testing.T) {
	called := false
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestClient_SendMessage_APIError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestClient_SendDocument_Success(t *testing.T) {
	var gotPath, gotFilename, gotCaption string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm() error = %v", err)
		}

		gotCaption = r.FormValue("caption")
		if fh := r.MultipartForm.File["document"]; len(fh) == 1 {
			gotFilename = fh[0].Filename
		}

		w.WriteHeader(http.StatusOK)
	})

	err := c.SendDocument(context.Background(), "123", strings.NewReader("stack trace content"), "stack.txt", "a caption")
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
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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
	c, _ := newTestClient(t, nil)
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
		{"message", func() error { return c.SendMessage(context.Background(), "123", "hi") }},
		{"document", func() error {
			return c.SendDocument(context.Background(), "123", strings.NewReader("doc"), "a.txt", "")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.send()
			if err == nil {
				t.Fatal("error = nil, want the transport error")
			}

			if strings.Contains(err.Error(), "secret-bot-token") || !strings.HasPrefix(err.Error(), "telegram: ") {
				t.Fatalf("error = %q, want a telegram: error without the token", err)
			}
		})
	}
}

// failingTransport fails every request.
type failingTransport struct{}

// RoundTrip implements http.RoundTripper.
func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}
