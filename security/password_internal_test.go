package security

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHasher_WaitsForAFreeSlot(t *testing.T) {
	ph, err := NewPasswordHasher(Options{Password: PasswordOptions{Cost: bcrypt.MinCost, MaxConcurrent: 1}})
	if err != nil {
		t.Fatal(err)
	}

	h := ph.(*passwordHasher)
	hash, _ := h.Hash("secret")

	release := h.acquire() // the only slot is busy
	done := make(chan bool)
	go func() { done <- h.Compare(hash, "secret") }()

	select {
	case <-done:
		t.Fatal("Compare ran while the only slot was taken")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	if ok := <-done; !ok {
		t.Error("Compare = false after the slot was freed, want true")
	}
}

func TestPasswordHasher_DefaultLimitIsAtLeastOne(t *testing.T) {
	ph, err := NewPasswordHasher(Options{Password: PasswordOptions{Cost: bcrypt.MinCost}})
	if err != nil {
		t.Fatal(err)
	}

	if c := cap(ph.(*passwordHasher).slots); c < 1 {
		t.Errorf("slots = %d, want >= 1", c)
	}
}
