package upload_test

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
		t.Fatal(err)
	}

	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	form, err := multipart.NewReader(&body, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}

	return form.File["file"][0]
}

func TestOpen_Rule(t *testing.T) {
	rule := upload.Image(1 << 10)

	tests := []struct {
		name     string
		filename string
		content  []byte
		ok       bool
	}{
		{"png", "avatar.PNG", pngHeader, true},
		{"text renamed to png", "avatar.png", []byte("<?php system($_GET['c']); ?>"), false},
		{"extension not allowed", "avatar.gif", pngHeader, false},
		{"too large", "avatar.png", append(pngHeader, make([]byte, 2<<10)...), false},
		{"empty", "avatar.png", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := upload.Open(fileHeader(t, tt.filename, tt.content), rule)
			if !tt.ok {
				if !errors.Is(err, upload.ErrInvalidFile) {
					t.Fatalf("err = %v, want ErrInvalidFile", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			defer file.Close()

			// The sniff must rewind, so the caller still reads the whole file.
			if got, _ := io.ReadAll(file); !bytes.Equal(got, tt.content) {
				t.Errorf("read %d bytes after Open, want the whole %d-byte file", len(got), len(tt.content))
			}
		})
	}
}

func TestHasType_Sniff(t *testing.T) {
	tests := []struct {
		name    string
		content string
		types   []string
		want    bool
	}{
		{"png matches", string(pngHeader), []string{"image/png"}, true},
		{"png not in list", string(pngHeader), []string{"image/jpeg"}, false},
		{"html is not an image", "<html><script>x</script>", []string{"image/png"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.content)
			if got := upload.HasType(r, tt.types...); got != tt.want {
				t.Fatalf("HasType() = %v, want %v", got, tt.want)
			}

			if r.Len() != len(tt.content) {
				t.Fatalf("HasType() left %d of %d bytes unread, want a rewind", r.Len(), len(tt.content))
			}
		})
	}
}

func TestExtension_Lowercase(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"a.PNG", ".png"},
		{"archive.tar.gz", ".gz"},
		{"noext", ""},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			if got := upload.Extension(tt.filename); got != tt.want {
				t.Fatalf("Extension(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}
