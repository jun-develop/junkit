package security

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestPasswordHasher_WaitsForAFreeSlot checks that a call waits while every slot is busy, gives
// up with ctx's error, and runs once a slot is free.
func TestPasswordHasher_WaitsForAFreeSlot(t *testing.T) {
	ph, err := NewPasswordHasher(Options{Password: PasswordOptions{Cost: bcrypt.MinCost, MaxConcurrent: 1}})
	if err != nil {
		t.Fatal(err)
	}

	h := ph.(*passwordHasher)
	hash, err := h.Hash(context.Background(), "secret")
	if err != nil {
		t.Fatal(err)
	}

	// Take the only slot so every call must wait.
	release, err := h.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := h.Hash(ctx, "secret"); !errors.Is(err, context.Canceled) {
		t.Errorf("Hash err = %v, want context.Canceled", err)
	}

	if ok, err := h.Compare(ctx, hash, "secret"); ok || !errors.Is(err, context.Canceled) {
		t.Errorf("Compare = %v, %v; want false, context.Canceled", ok, err)
	}

	if err := h.CompareDummy(ctx, "secret"); !errors.Is(err, context.Canceled) {
		t.Errorf("CompareDummy err = %v, want context.Canceled", err)
	}

	release()

	if ok, err := h.Compare(context.Background(), hash, "secret"); !ok || err != nil {
		t.Errorf("Compare after release = %v, %v; want true, nil", ok, err)
	}
}

// TestPasswordHasher_DefaultLimitIsAtLeastOne checks the MaxConcurrent default.
func TestPasswordHasher_DefaultLimitIsAtLeastOne(t *testing.T) {
	ph, err := NewPasswordHasher(Options{Password: PasswordOptions{Cost: bcrypt.MinCost}})
	if err != nil {
		t.Fatal(err)
	}

	if c := cap(ph.(*passwordHasher).slots); c < 1 {
		t.Errorf("slots = %d, want >= 1", c)
	}
}
