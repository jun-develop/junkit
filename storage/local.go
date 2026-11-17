package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// unsafeExtensions are extensions a browser runs as active content when the file is served from
// the app's origin, which would turn an upload into stored XSS.
var unsafeExtensions = []string{
	".html", ".htm", ".xhtml", ".xht", ".shtml", ".svg", ".svgz",
	".js", ".mjs", ".xml", ".xsl", ".xslt", ".swf",
}

// LocalService implements Service using the local filesystem.
type LocalService struct {
	baseDir string
	baseURL string
}

// NewLocalService creates a LocalService, creating opts.BaseDir if it does not exist.
//
// Usage:
//
//	svc, err := storage.NewLocalService(storage.Options{BaseDir: "./uploads", BaseURL: publicURL + "/uploads"})
func NewLocalService(opts Options) (*LocalService, error) {
	if err := os.MkdirAll(opts.BaseDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("storage: create base dir: %w", err)
	}

	return &LocalService{baseDir: opts.BaseDir, baseURL: strings.TrimSuffix(opts.BaseURL, "/")}, nil
}

// UploadFile implements Service; the file is stored under a generated unique filename that keeps
// the lowercased original extension. Missing or active-content extensions get ErrUnsafeExtension.
func (s *LocalService) UploadFile(ctx context.Context, file io.Reader, originalFilename string) (string, error) {
	ext := strings.ToLower(filepath.Ext(originalFilename))
	// shortcut: a denylist, so an unknown extension is still content-sniffed when served; pass
	// files through upload.Open (an allowlist) before storing them.
	if ext == "" || slices.Contains(unsafeExtensions, ext) {
		return "", ErrUnsafeExtension
	}

	uniqueFilename := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), ext)
	dst := filepath.Join(s.baseDir, uniqueFilename)

	out, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("storage: create file: %w", err)
	}

	if _, err := io.Copy(out, ctxReader{ctx: ctx, r: file}); err != nil {
		// Best-effort cleanup: the copy error is the one worth returning.
		_ = out.Close()
		_ = os.Remove(dst)

		return "", fmt.Errorf("storage: copy file: %w", err)
	}

	// Close is checked: a failed final flush means the stored file is incomplete.
	if err := out.Close(); err != nil {
		// Best-effort cleanup of the incomplete file.
		_ = os.Remove(dst)

		return "", fmt.Errorf("storage: close file: %w", err)
	}

	return s.baseURL + "/" + uniqueFilename, nil
}

// DeleteFile implements Service; fileURL must be BaseURL + "/" + a file name. Deleting a file
// that does not exist is not an error.
func (s *LocalService) DeleteFile(_ context.Context, fileURL string) error {
	filename, ok := strings.CutPrefix(fileURL, s.baseURL+"/")
	if !ok || filename == "" || filename == "." || filename == ".." || strings.ContainsAny(filename, `/\`) {
		return ErrInvalidURL
	}

	if err := os.Remove(filepath.Join(s.baseDir, filename)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete file: %w", err)
	}

	return nil
}

// ctxReader fails reads once ctx is done, so a cancelled request stops a long copy.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

// Read implements io.Reader.
func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.r.Read(p)
}
