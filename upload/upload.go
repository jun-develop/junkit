// Package upload checks an uploaded file (size, extension, sniffed content type) before it is stored.
package upload

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
)

// ErrInvalidFile is returned when an upload breaks its Rule.
var ErrInvalidFile = errors.New("upload: invalid file")

// Rule is what an uploaded file must satisfy.
type Rule struct {
	// MaxSize is the largest allowed size in bytes.
	MaxSize int64

	// Extensions are the allowed lowercase extensions, with the leading dot.
	Extensions []string

	// Types are the allowed content types, as http.DetectContentType reports them.
	Types []string
}

// Image allows JPEG, PNG and WebP images up to maxSize bytes.
func Image(maxSize int64) Rule {
	return Rule{
		MaxSize:    maxSize,
		Extensions: []string{".jpg", ".jpeg", ".png", ".webp"},
		Types:      []string{"image/jpeg", "image/png", "image/webp"},
	}
}

// Open checks fh against rule and opens it; the caller closes the file. The content is sniffed as
// well, so a file whose extension is allowed but whose bytes are something else is refused.
func Open(fh *multipart.FileHeader, rule Rule) (multipart.File, error) {
	if fh.Size > rule.MaxSize || !slices.Contains(rule.Extensions, Extension(fh.Filename)) {
		return nil, ErrInvalidFile
	}

	file, err := fh.Open()
	if err != nil {
		return nil, ErrInvalidFile
	}

	if !HasType(file, rule.Types...) {
		_ = file.Close()
		return nil, ErrInvalidFile
	}

	return file, nil
}

// HasType reports whether file's content is one of types, then rewinds it.
func HasType(file io.ReadSeeker, types ...string) bool {
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false
	}

	return slices.Contains(types, http.DetectContentType(head[:n]))
}

// Extension returns filename's lowercase extension, including the leading dot.
func Extension(filename string) string {
	return strings.ToLower(filepath.Ext(filename))
}
