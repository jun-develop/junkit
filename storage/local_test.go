package storage_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: local_test.go
// Purpose: Verify the local-disk storage service: directory creation, uploads and safe deletes.
// Type: Unit (uses a temporary directory)
//
// Test functions: 7
// Total scenarios: 19
//
// #   Test Case                                            Scenario                                  Expected Result
// 1   TestNewLocalService_CreatesBaseDir                   Base directory does not exist             Directory is created
// 2   TestNewLocalService_FailsWhenBaseDirCannotBeCreated  Parent of the base dir is a regular file  Returns an error and a nil service
// 3   TestLocalService_UploadFile_ValidatesExtension       .png file                                 Stored under the base URL with .png
// 4   TestLocalService_UploadFile_ValidatesExtension       .JPG file                                 Stored with the extension lowercased
// 5   TestLocalService_UploadFile_ValidatesExtension       .html file                                Returns ErrUnsafeExtension
// 6   TestLocalService_UploadFile_ValidatesExtension       .SVG file                                 Returns ErrUnsafeExtension
// 7   TestLocalService_UploadFile_ValidatesExtension       .mjs file                                 Returns ErrUnsafeExtension
// 8   TestLocalService_UploadFile_ValidatesExtension       No extension                              Returns ErrUnsafeExtension
// 9   TestLocalService_UploadFile_GeneratesUniqueNames     Same filename uploaded twice              Two different URLs
// 10  TestLocalService_UploadFile_StopsWhenCancelled       Cancelled context                         Returns context.Canceled; no file left behind
// 11  TestLocalService_DeleteFile_DeletesOnlySafeURLs      URL of an uploaded file                   File is deleted
// 12  TestLocalService_DeleteFile_DeletesOnlySafeURLs      URL of a missing file                     Returns nil
// 13  TestLocalService_DeleteFile_DeletesOnlySafeURLs      URL on another host                       Returns ErrInvalidURL; file kept
// 14  TestLocalService_DeleteFile_DeletesOnlySafeURLs      Empty URL                                 Returns ErrInvalidURL; file kept
// 15  TestLocalService_DeleteFile_DeletesOnlySafeURLs      Bare base URL                             Returns ErrInvalidURL; file kept
// 16  TestLocalService_DeleteFile_DeletesOnlySafeURLs      Path "/.."                                Returns ErrInvalidURL; file kept
// 17  TestLocalService_DeleteFile_DeletesOnlySafeURLs      Nested path "/x/.."                       Returns ErrInvalidURL; file kept
// 18  TestLocalService_DeleteFile_DeletesOnlySafeURLs      Path with a backslash                     Returns ErrInvalidURL; file kept
// 19  TestLocalService_DeleteFile_ReturnsRemovalError      Path is a non-empty directory             Returns an error other than ErrInvalidURL
//
// =============================================================================

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

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("os.Stat(%s) error = %v, want the directory created", dir, err)
	}

	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
}

func TestNewLocalService_FailsWhenBaseDirCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	svc, err := storage.NewLocalService(storage.Options{BaseDir: filepath.Join(blocker, "uploads")})

	if err == nil {
		t.Fatal("NewLocalService() error = nil, want an error when a parent is a regular file")
	}

	if svc != nil {
		t.Errorf("NewLocalService() = %v, want nil on error", svc)
	}
}

func TestLocalService_UploadFile_ValidatesExtension(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantExt  string
		wantErr  error
	}{
		{name: "png file keeps its extension", filename: "avatar.png", wantExt: ".png"},
		{name: "uppercase extension is lowercased", filename: "avatar.JPG", wantExt: ".jpg"},
		{name: "html file is rejected", filename: "page.html", wantErr: storage.ErrUnsafeExtension},
		{name: "svg file is rejected in any case", filename: "logo.SVG", wantErr: storage.ErrUnsafeExtension},
		{name: "javascript module is rejected", filename: "app.mjs", wantErr: storage.ErrUnsafeExtension},
		{name: "file without extension is rejected", filename: "avatar", wantErr: storage.ErrUnsafeExtension},
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

			if !strings.HasPrefix(url, baseURL+"/") {
				t.Fatalf("UploadFile() url = %q, want it under %s/", url, baseURL)
			}

			if strings.HasPrefix(url, baseURL+"//") {
				t.Fatalf("UploadFile() url = %q, want a single slash after %s", url, baseURL)
			}

			if filepath.Ext(url) != tt.wantExt {
				t.Fatalf("UploadFile() url = %q, want extension %s", url, tt.wantExt)
			}

			data, err := os.ReadFile(filepath.Join(dir, filepath.Base(url)))
			if err != nil {
				t.Fatalf("read stored file error = %v", err)
			}

			if string(data) != "bytes" {
				t.Fatalf("stored file = %q, want %q", data, "bytes")
			}
		})
	}
}

func TestLocalService_UploadFile_GeneratesUniqueNames(t *testing.T) {
	svc, _ := newService(t)

	url1, err := svc.UploadFile(context.Background(), strings.NewReader("a"), "same-name.jpg")
	if err != nil {
		t.Fatalf("first UploadFile() error = %v", err)
	}

	url2, err := svc.UploadFile(context.Background(), strings.NewReader("b"), "same-name.jpg")
	if err != nil {
		t.Fatalf("second UploadFile() error = %v", err)
	}

	if url1 == url2 {
		t.Fatalf("two uploads of the same filename produced the same URL: %q", url1)
	}
}

func TestLocalService_UploadFile_StopsWhenCancelled(t *testing.T) {
	svc, dir := newService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.UploadFile(ctx, strings.NewReader("a"), "a.png"); !errors.Is(err, context.Canceled) {
		t.Fatalf("UploadFile() error = %v, want context.Canceled", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir(%s) error = %v", dir, err)
	}

	if len(entries) != 0 {
		t.Fatalf("cancelled upload left %d files behind", len(entries))
	}
}

func TestLocalService_DeleteFile_DeletesOnlySafeURLs(t *testing.T) {
	tests := []struct {
		name    string
		url     func(uploaded string) string
		wantErr error
		gone    bool
	}{
		{name: "deletes an uploaded file", url: func(u string) string { return u }, gone: true},
		{name: "missing file is not an error", url: func(string) string { return baseURL + "/missing.png" }},
		{name: "url on another host is rejected", wantErr: storage.ErrInvalidURL,
			url: func(u string) string { return "http://evil.example/uploads/" + filepath.Base(u) }},
		{name: "empty url is rejected", url: func(string) string { return "" }, wantErr: storage.ErrInvalidURL},
		{name: "bare base url is rejected", url: func(string) string { return baseURL + "/" }, wantErr: storage.ErrInvalidURL},
		{name: "parent directory path is rejected", url: func(string) string { return baseURL + "/.." }, wantErr: storage.ErrInvalidURL},
		{name: "nested path is rejected", url: func(string) string { return baseURL + "/x/.." }, wantErr: storage.ErrInvalidURL},
		{name: "path with a backslash is rejected", url: func(string) string { return baseURL + `/..\x` }, wantErr: storage.ErrInvalidURL},
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
				t.Fatalf("uploaded file deleted = %v, want %v", gone, tt.gone)
			}
		})
	}
}

func TestLocalService_DeleteFile_ReturnsRemovalError(t *testing.T) {
	svc, dir := newService(t)

	// os.Remove fails on a non-empty directory with an error other than "not exist".
	nested := filepath.Join(dir, "not-a-plain-file")
	if err := os.MkdirAll(filepath.Join(nested, "child"), 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	err := svc.DeleteFile(context.Background(), baseURL+"/not-a-plain-file")
	if err == nil {
		t.Fatal("DeleteFile() error = nil, want a removal error")
	}

	if errors.Is(err, storage.ErrInvalidURL) {
		t.Fatalf("DeleteFile() error = %v, want a removal error, not ErrInvalidURL", err)
	}
}
