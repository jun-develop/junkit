// Package storage provides file upload and deletion abstractions.
package storage

import (
	"context"
	"io"
)

// Service uploads and deletes files. UploadFile stores file and returns its URL or path;
// DeleteFile removes a previously uploaded file given the URL UploadFile returned.
type Service interface {
	UploadFile(ctx context.Context, file io.Reader, originalFilename string) (string, error)
	DeleteFile(ctx context.Context, fileURL string) error
}

// Options configures local file storage.
type Options struct {
	// BaseDir is the local directory files are written to, e.g. "./uploads".
	BaseDir string

	// BaseURL is the URL prefix under which BaseDir is served, e.g. "http://localhost:8080/uploads".
	BaseURL string
}
