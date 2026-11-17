// Package telegram provides a Telegram Bot API client for sending messages and documents.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// maxErrorBody caps how much of a failed response body is read into the returned error.
const maxErrorBody = 4 << 10

// Client sends Telegram messages and documents using HTML parse mode. Both methods return nil
// without sending when the token or chatID is empty (SendDocument also when content is nil), so an
// unconfigured bot is a silent no-op. SendDocument streams content, so memory use stays constant
// regardless of file size. Returned errors never contain the bot token.
type Client interface {
	SendMessage(ctx context.Context, chatID, message string) error
	SendDocument(ctx context.Context, chatID string, content io.Reader, filename string, caption string) error
}

// Options configures the Telegram client.
type Options struct {
	// Token is the Telegram Bot API token; an empty Token skips sending.
	Token string
}

// client is the default implementation of Client.
type client struct {
	httpClient *http.Client
	botPrefix  string
	token      string
}

// sendMessageRequest is the JSON body for Telegram's sendMessage endpoint.
type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// NewClient creates a Client; a nil httpClient falls back to one with a 10s timeout.
//
// Usage:
//
//	c := telegram.NewClient(telegram.Options{Token: token}, nil)
func NewClient(opts Options, httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &client{
		httpClient: httpClient,
		botPrefix:  "https://api.telegram.org/bot" + opts.Token,
		token:      opts.Token,
	}
}

// SendMessage implements Client; it posts a JSON body to the sendMessage endpoint.
func (c *client) SendMessage(ctx context.Context, chatID, message string) error {
	if c.token == "" || chatID == "" {
		return nil
	}

	body, err := json.Marshal(sendMessageRequest{ChatID: chatID, Text: message, ParseMode: "HTML"})
	if err != nil {
		return fmt.Errorf("telegram: send message: %w", err)
	}

	return c.post(ctx, "sendMessage", "application/json", bytes.NewReader(body))
}

// SendDocument implements Client; it streams the multipart body through an io.Pipe.
func (c *client) SendDocument(ctx context.Context, chatID string, content io.Reader, filename string, caption string) error {
	if c.token == "" || chatID == "" || content == nil {
		return nil
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		err := writeDocument(writer, chatID, content, filename, caption)
		if err == nil {
			err = writer.Close()
		}

		// CloseWithError always returns nil.
		_ = pw.CloseWithError(err)
	}()

	err := c.post(ctx, "sendDocument", writer.FormDataContentType(), pr)

	// Unblocks the writer goroutine when the request ended before reading the whole body;
	// CloseWithError always returns nil.
	_ = pr.CloseWithError(errors.New("telegram: request finished"))

	return err
}

// writeDocument writes the sendDocument form fields and the document to writer.
func writeDocument(writer *multipart.Writer, chatID string, content io.Reader, filename, caption string) error {
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}

	if caption != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}

		if err := writer.WriteField("parse_mode", "HTML"); err != nil {
			return err
		}
	}

	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return err
	}

	_, err = io.Copy(part, content)

	return err
}

// post sends body to the Bot API method and returns an error for a failed or non-200 response.
// Transport errors are unwrapped from *url.Error, whose text holds the URL and so the bot token.
func (c *client) post(ctx context.Context, method, contentType string, body io.Reader) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.botPrefix+"/"+method, body)
	if err != nil {
		return fmt.Errorf("telegram: %s: build request failed", method)
	}

	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}

		return fmt.Errorf("telegram: %s: %w", method, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// The body only adds detail to the error; a failed read still reports the status.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("telegram: %s: status %d: %s", method, resp.StatusCode, msg)
	}

	return nil
}
