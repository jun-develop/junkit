package security_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/jun-develop/junkit/security"
)

// newHasher returns a PasswordHasher with the cheapest bcrypt cost.
func newHasher(t *testing.T) security.PasswordHasher {
	t.Helper()
	h, err := security.NewPasswordHasher(security.Options{Password: security.PasswordOptions{Cost: bcrypt.MinCost}})
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}

	return h
}

// TestPasswordHasher_HashAndCompare checks that Compare accepts only the hashed password.
func TestPasswordHasher_HashAndCompare(t *testing.T) {
	ctx := context.Background()
	h := newHasher(t)

	hash, err := h.Hash(ctx, "correct horse")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if hash == "correct horse" {
		t.Fatal("hash equals plaintext")
	}

	tests := []struct {
		name, hash, plain string
		want              bool
	}{
		{"correct", hash, "correct horse", true},
		{"wrong", hash, "wrong horse", false},
		{"garbage hash", "not-a-bcrypt-hash", "correct horse", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.Compare(ctx, tt.hash, tt.plain)
			if err != nil || got != tt.want {
				t.Errorf("Compare = %v, %v; want %v, nil", got, err, tt.want)
			}
		})
	}
}

// TestPasswordHasher_UsesConfiguredCost checks that hashes carry the configured cost.
func TestPasswordHasher_UsesConfiguredCost(t *testing.T) {
	hash, err := newHasher(t).Hash(context.Background(), "pw")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil || cost != bcrypt.MinCost {
		t.Errorf("cost = %d, %v; want %d", cost, err, bcrypt.MinCost)
	}
}

// TestPasswordHasher_TooLong checks that passwords over 72 bytes are rejected, not truncated.
func TestPasswordHasher_TooLong(t *testing.T) {
	ctx := context.Background()
	h := newHasher(t)
	long := strings.Repeat("a", 73)

	if _, err := h.Hash(ctx, long); !errors.Is(err, security.ErrPasswordTooLong) {
		t.Errorf("Hash(73 bytes) err = %v, want ErrPasswordTooLong", err)
	}

	// A hash of the 72-byte prefix must not match the 73-byte input (no silent truncation).
	hash, err := h.Hash(ctx, long[:72])
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if ok, err := h.Compare(ctx, hash, long); ok || err != nil {
		t.Errorf("Compare(73 bytes) = %v, %v; want false, nil", ok, err)
	}
}

// TestPasswordHasher_CompareDummy checks that CompareDummy accepts any input length.
func TestPasswordHasher_CompareDummy(t *testing.T) {
	h := newHasher(t)
	for _, plain := range []string{"", strings.Repeat("x", 200)} {
		t.Run(plain, func(t *testing.T) {
			if err := h.CompareDummy(context.Background(), plain); err != nil {
				t.Errorf("CompareDummy: %v", err)
			}
		})
	}
}

// TestNewPasswordHasher_InvalidCost checks that out-of-range costs are rejected.
func TestNewPasswordHasher_InvalidCost(t *testing.T) {
	for _, cost := range []int{1, 32, -1} {
		opts := security.Options{Password: security.PasswordOptions{Cost: cost}}
		if _, err := security.NewPasswordHasher(opts); !errors.Is(err, security.ErrInvalidPasswordCost) {
			t.Errorf("cost %d: err = %v, want ErrInvalidPasswordCost", cost, err)
		}
	}
}
