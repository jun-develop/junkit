package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/storage"
)

// baseURL is the public prefix every test service serves files under.
const baseURL = "http://localhost:8080/uploads"

// newService returns a LocalService rooted at a fresh directory, and that directory.
func newService(t *testing.T) (*storage.LocalService, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "uploads")
	svc, err := storage.NewLocalService(storage.Options{BaseDir: dir, BaseURL: baseURL + "/"})
	if err != nil {
		t.Fatalf("NewLocalService() error = %v", err)
	}

	return svc, dir
}

func TestNewLocalService_CreatesBaseDir(t *testing.T) {
	_, dir := newService(t)

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("NewLocalService() did not create %s as a directory", dir)
	}
}

func TestLocalService_UploadFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantExt  string
		wantErr  error
	}{
		{name: "keeps the extension", filename: "avatar.png", wantExt: ".png"},
		{name: "lowercases the extension", filename: "avatar.JPG", wantExt: ".jpg"},
		{name: "rejects html", filename: "page.html", wantErr: storage.ErrUnsafeExtension},
		{name: "rejects svg in any case", filename: "logo.SVG", wantErr: storage.ErrUnsafeExtension},
		{name: "rejects js", filename: "app.mjs", wantErr: storage.ErrUnsafeExtension},
		{name: "rejects no extension", filename: "avatar", wantErr: storage.ErrUnsafeExtension},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dir := newService(t)

			url, err := svc.UploadFile(context.Background(), strings.NewReader("bytes"), tt.filename)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UploadFile() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr != nil {
				return
			}

			if !strings.HasPrefix(url, baseURL+"/") || strings.HasPrefix(url, baseURL+"//") {
				t.Fatalf("UploadFile() url = %q, want it under %s/", url, baseURL)
			}

			if filepath.Ext(url) != tt.wantExt {
				t.Fatalf("UploadFile() url = %q, want extension %s", url, tt.wantExt)
			}

			data, err := os.ReadFile(filepath.Join(dir, filepath.Base(url)))
			if err != nil || string(data) != "bytes" {
				t.Fatalf("stored file = %q, %v; want %q", data, err, "bytes")
			}
		})
	}
}

func TestLocalService_UploadFileUniqueNames(t *testing.T) {
	svc, _ := newService(t)

	url1, err1 := svc.UploadFile(context.Background(), strings.NewReader("a"), "same-name.jpg")
	url2, err2 := svc.UploadFile(context.Background(), strings.NewReader("b"), "same-name.jpg")
	if err1 != nil || err2 != nil {
		t.Fatalf("UploadFile() errors = %v, %v", err1, err2)
	}

	if url1 == url2 {
		t.Fatalf("two uploads of the same filename produced the same URL: %q", url1)
	}
}

func TestLocalService_UploadFileCancelled(t *testing.T) {
	svc, dir := newService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.UploadFile(ctx, strings.NewReader("a"), "a.png"); !errors.Is(err, context.Canceled) {
		t.Fatalf("UploadFile() error = %v, want context.Canceled", err)
	}

	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("cancelled upload left %d files behind", len(entries))
	}
}

func TestLocalService_DeleteFile(t *testing.T) {
	tests := []struct {
		name    string
		url     func(uploaded string) string
		wantErr error
		gone    bool
	}{
		{name: "deletes an uploaded file", url: func(u string) string { return u }, gone: true},
		{name: "missing file is not an error", url: func(string) string { return baseURL + "/missing.png" }},
		{name: "other host is rejected", wantErr: storage.ErrInvalidURL,
			url: func(u string) string { return "http://evil.example/uploads/" + filepath.Base(u) }},
		{name: "empty", url: func(string) string { return "" }, wantErr: storage.ErrInvalidURL},
		{name: "bare base URL", url: func(string) string { return baseURL + "/" }, wantErr: storage.ErrInvalidURL},
		{name: "dot dot", url: func(string) string { return baseURL + "/.." }, wantErr: storage.ErrInvalidURL},
		{name: "nested path", url: func(string) string { return baseURL + "/x/.." }, wantErr: storage.ErrInvalidURL},
		{name: "backslash", url: func(string) string { return baseURL + `/..\x` }, wantErr: storage.ErrInvalidURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dir := newService(t)
			uploaded, err := svc.UploadFile(context.Background(), strings.NewReader("x"), "a.txt")
			if err != nil {
				t.Fatalf("UploadFile() error = %v", err)
			}

			if err := svc.DeleteFile(context.Background(), tt.url(uploaded)); !errors.Is(err, tt.wantErr) {
				t.Fatalf("DeleteFile() error = %v, want %v", err, tt.wantErr)
			}

			_, statErr := os.Stat(filepath.Join(dir, filepath.Base(uploaded)))
			if gone := os.IsNotExist(statErr); gone != tt.gone {
				t.Fatalf("file gone = %v, want %v", gone, tt.gone)
			}
		})
	}
}

func TestLocalService_DeleteFileRemovalError(t *testing.T) {
	svc, dir := newService(t)

	// os.Remove fails on a non-empty directory with an error other than "not exist".
	nested := filepath.Join(dir, "not-a-plain-file")
	if err := os.MkdirAll(filepath.Join(nested, "child"), 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	err := svc.DeleteFile(context.Background(), baseURL+"/not-a-plain-file")
	if err == nil || errors.Is(err, storage.ErrInvalidURL) {
		t.Fatalf("DeleteFile() error = %v, want a removal error", err)
	}
}
