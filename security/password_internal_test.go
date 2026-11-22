package security

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: password_internal_test.go
// Purpose: Verify the concurrency limit (slots) inside the bcrypt password hasher.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 2
//
// #  Test Case                                    Scenario                                    Expected Result
// 1  TestPasswordHasher_WaitsForAFreeSlot         Every slot busy, then the slot is released  Calls fail with context.Canceled, then succeed
// 2  TestPasswordHasher_DefaultLimitIsAtLeastOne  MaxConcurrent not set                       At least one slot is available
//
// =============================================================================

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHasher_WaitsForAFreeSlot(t *testing.T) {
	// Arrange: a hasher with one slot, a valid hash, and the only slot taken.
	ph, err := NewPasswordHasher(Options{Password: PasswordOptions{Cost: bcrypt.MinCost, MaxConcurrent: 1}})
	if err != nil {
		t.Fatalf("NewPasswordHasher() error = %v", err)
	}

	h := ph.(*passwordHasher)
	hash, err := h.Hash(context.Background(), "secret")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	release, err := h.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	// Act + Assert: while the slot is busy, every call gives up with the context error.
	if _, err := h.Hash(canceled, "secret"); !errors.Is(err, context.Canceled) {
		t.Errorf("Hash() while busy error = %v, want context.Canceled", err)
	}

	ok, err := h.Compare(canceled, hash, "secret")
	if ok {
		t.Error("Compare() while busy = true, want false")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Compare() while busy error = %v, want context.Canceled", err)
	}

	if err := h.CompareDummy(canceled, "secret"); !errors.Is(err, context.Canceled) {
		t.Errorf("CompareDummy() while busy error = %v, want context.Canceled", err)
	}

	// Act + Assert: once the slot is free, calls run normally.
	release()

	ok, err = h.Compare(context.Background(), hash, "secret")
	if err != nil {
		t.Fatalf("Compare() after release error = %v, want nil", err)
	}

	if !ok {
		t.Error("Compare() after release = false, want true")
	}
}

func TestPasswordHasher_DefaultLimitIsAtLeastOne(t *testing.T) {
	ph, err := NewPasswordHasher(Options{Password: PasswordOptions{Cost: bcrypt.MinCost}})
	if err != nil {
		t.Fatalf("NewPasswordHasher() error = %v", err)
	}

	if slots := cap(ph.(*passwordHasher).slots); slots < 1 {
		t.Errorf("slot count = %d, want >= 1", slots)
	}
}
