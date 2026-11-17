package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/jun-develop/junkit/storage"
)

func TestNewService_ReturnsService(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uploads")

	svc, err := storage.NewService(storage.Options{BaseDir: dir, BaseURL: "http://localhost/uploads"})
	if err != nil || svc == nil {
		t.Fatalf("NewService() = %v, %v; want a Service", svc, err)
	}
}
