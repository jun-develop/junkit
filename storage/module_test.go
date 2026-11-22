package storage_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: module_test.go
// Purpose: Verify the Fx-facing constructor NewService.
// Type: Unit (uses a temporary directory)
//
// Test functions: 1
// Total scenarios: 1
//
// #  Test Case                      Scenario                      Expected Result
// 1  TestNewService_ReturnsService  Valid base directory and URL  Returns a non-nil Service without error
//
// =============================================================================

import (
	"path/filepath"
	"testing"

	"github.com/jun-develop/junkit/storage"
)

func TestNewService_ReturnsService(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "uploads")

	svc, err := storage.NewService(storage.Options{BaseDir: dir, BaseURL: "http://localhost/uploads"})

	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if svc == nil {
		t.Fatal("NewService() = nil, want a Service")
	}
}
