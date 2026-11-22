package upload_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: upload_test.go
// Purpose: Verify validation of uploaded files by extension, size and sniffed content type.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 11
//
// #   Test Case                                Scenario                               Expected Result
// 1   TestOpen_AcceptsValidImage               PNG with an upper-case .PNG extension  Opens; the whole file is still readable
// 2   TestOpen_RejectsInvalidFile              PHP text renamed to .png               Returns ErrInvalidFile
// 3   TestOpen_RejectsInvalidFile              Extension not allowed (.gif)           Returns ErrInvalidFile
// 4   TestOpen_RejectsInvalidFile              File larger than the limit             Returns ErrInvalidFile
// 5   TestOpen_RejectsInvalidFile              Empty file                             Returns ErrInvalidFile
// 6   TestHasType_SniffsContentAndRewinds      PNG content, image/png allowed         Returns true; reader rewound
// 7   TestHasType_SniffsContentAndRewinds      PNG content, only image/jpeg allowed   Returns false; reader rewound
// 8   TestHasType_SniffsContentAndRewinds      HTML content, image/png allowed        Returns false; reader rewound
// 9   TestExtension_ReturnsLowercaseExtension  "a.PNG"                                Returns ".png"
// 10  TestExtension_ReturnsLowercaseExtension  "archive.tar.gz"                       Returns only the last extension ".gz"
// 11  TestExtension_ReturnsLowercaseExtension  No extension                           Returns an empty string
//
// =============================================================================

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/upload"
)

// pngHeader is the start of a PNG file, enough for http.DetectContentType.
var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

// fileHeader builds the *multipart.FileHeader a handler gets from c.FormFile.
func fileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}

	if _, err := part.Write(content); err != nil {
		t.Fatalf("write form file error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer error = %v", err)
	}

	form, err := multipart.NewReader(&body, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm() error = %v", err)
	}

	return form.File["file"][0]
}

func TestOpen_AcceptsValidImage(t *testing.T) {
	rule := upload.Image(1 << 10)

	file, err := upload.Open(fileHeader(t, "avatar.PNG", pngHeader), rule)
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer file.Close()

	// Open sniffs the content, so it must rewind for the caller to read the whole file.
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read after Open() error = %v", err)
	}

	if !bytes.Equal(got, pngHeader) {
		t.Errorf("read %d bytes after Open(), want the whole %d-byte file", len(got), len(pngHeader))
	}
}

func TestOpen_RejectsInvalidFile(t *testing.T) {
	rule := upload.Image(1 << 10)

	tests := []struct {
		name     string
		filename string
		content  []byte
	}{
		{"php text renamed to png", "avatar.png", []byte("<?php system($_GET['c']); ?>")},
		{"extension not allowed", "avatar.gif", pngHeader},
		{"file larger than the limit", "avatar.png", append(pngHeader, make([]byte, 2<<10)...)},
		{"empty file", "avatar.png", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := upload.Open(fileHeader(t, tt.filename, tt.content), rule)

			if !errors.Is(err, upload.ErrInvalidFile) {
				t.Errorf("Open(%s) error = %v, want ErrInvalidFile", tt.filename, err)
			}
		})
	}
}

func TestHasType_SniffsContentAndRewinds(t *testing.T) {
	tests := []struct {
		name    string
		content string
		types   []string
		want    bool
	}{
		{"png content, image/png allowed", string(pngHeader), []string{"image/png"}, true},
		{"png content, only image/jpeg allowed", string(pngHeader), []string{"image/jpeg"}, false},
		{"html content, image/png allowed", "<html><script>x</script>", []string{"image/png"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.content)
			got := upload.HasType(r, tt.types...)

			if got != tt.want {
				t.Errorf("HasType(%v) = %v, want %v", tt.types, got, tt.want)
			}

			if r.Len() != len(tt.content) {
				t.Errorf("HasType() left %d of %d bytes unread, want a rewind", r.Len(), len(tt.content))
			}
		})
	}
}

func TestExtension_ReturnsLowercaseExtension(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{"upper-case extension", "a.PNG", ".png"},
		{"double extension keeps the last one", "archive.tar.gz", ".gz"},
		{"no extension", "noext", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upload.Extension(tt.filename); got != tt.want {
				t.Errorf("Extension(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}
