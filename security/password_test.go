package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: password_test.go
// Purpose: Verify bcrypt password hashing, comparison and option validation.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 11
//
// #   Test Case                                        Scenario                              Expected Result
// 1   TestPasswordHasher_ComparesAgainstHash           Correct password                      Returns true
// 2   TestPasswordHasher_ComparesAgainstHash           Wrong password                        Returns false
// 3   TestPasswordHasher_ComparesAgainstHash           Hash is not a bcrypt hash             Returns false without an error
// 4   TestPasswordHasher_UsesConfiguredCost            Hash with MinCost configured          Hash carries bcrypt.MinCost
// 5   TestPasswordHasher_RejectsPasswordsOver72Bytes   Hash a 73-byte password               Returns ErrPasswordTooLong
// 6   TestPasswordHasher_RejectsPasswordsOver72Bytes   73-byte input vs. its 72-byte prefix  Does not match (no silent truncation)
// 7   TestPasswordHasher_CompareDummyAcceptsAnyLength  Empty password                        Returns no error
// 8   TestPasswordHasher_CompareDummyAcceptsAnyLength  200-byte password                     Returns no error
// 9   TestNewPasswordHasher_RejectsInvalidCost         Cost below bcrypt.MinCost (1)         Returns ErrInvalidPasswordCost
// 10  TestNewPasswordHasher_RejectsInvalidCost         Cost above bcrypt.MaxCost (32)        Returns ErrInvalidPasswordCost
// 11  TestNewPasswordHasher_RejectsInvalidCost         Negative cost                         Returns ErrInvalidPasswordCost
//
// =============================================================================

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/jun-develop/junkit/security"
)

// newHasher returns a PasswordHasher with the cheapest bcrypt cost, to keep tests fast.
func newHasher(t *testing.T) security.PasswordHasher {
	t.Helper()
	h, err := security.NewPasswordHasher(security.Options{Password: security.PasswordOptions{Cost: bcrypt.MinCost}})
	if err != nil {
		t.Fatalf("NewPasswordHasher() error = %v", err)
	}

	return h
}

func TestPasswordHasher_ComparesAgainstHash(t *testing.T) {
	ctx := context.Background()
	h := newHasher(t)

	hash, err := h.Hash(ctx, "correct horse")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	if hash == "correct horse" {
		t.Fatal("Hash() returned the plaintext password")
	}

	tests := []struct {
		name  string
		hash  string
		plain string
		want  bool
	}{
		{"correct password", hash, "correct horse", true},
		{"wrong password", hash, "wrong horse", false},
		{"hash is not a bcrypt hash", "not-a-bcrypt-hash", "correct horse", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.Compare(ctx, tt.hash, tt.plain)

			if err != nil {
				t.Fatalf("Compare() error = %v, want nil", err)
			}

			if got != tt.want {
				t.Errorf("Compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPasswordHasher_UsesConfiguredCost(t *testing.T) {
	hash, err := newHasher(t).Hash(context.Background(), "pw")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	cost, err := bcrypt.Cost([]byte(hash))

	if err != nil {
		t.Fatalf("bcrypt.Cost() error = %v", err)
	}

	if cost != bcrypt.MinCost {
		t.Errorf("hash cost = %d, want %d", cost, bcrypt.MinCost)
	}
}

func TestPasswordHasher_RejectsPasswordsOver72Bytes(t *testing.T) {
	ctx := context.Background()
	h := newHasher(t)
	long := strings.Repeat("a", 73)

	t.Run("hash a 73-byte password", func(t *testing.T) {
		if _, err := h.Hash(ctx, long); !errors.Is(err, security.ErrPasswordTooLong) {
			t.Errorf("Hash(73 bytes) error = %v, want ErrPasswordTooLong", err)
		}
	})

	t.Run("73-byte input vs. its 72-byte prefix", func(t *testing.T) {
		hash, err := h.Hash(ctx, long[:72])
		if err != nil {
			t.Fatalf("Hash(72 bytes) error = %v", err)
		}

		ok, err := h.Compare(ctx, hash, long)

		if err != nil {
			t.Errorf("Compare(73 bytes) error = %v, want nil", err)
		}

		if ok {
			t.Error("Compare(73 bytes) = true, want false (the input must not be truncated)")
		}
	})
}

func TestPasswordHasher_CompareDummyAcceptsAnyLength(t *testing.T) {
	h := newHasher(t)
	tests := []struct {
		name  string
		plain string
	}{
		{"empty password", ""},
		{"200-byte password", strings.Repeat("x", 200)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := h.CompareDummy(context.Background(), tt.plain); err != nil {
				t.Errorf("CompareDummy() error = %v, want nil", err)
			}
		})
	}
}

func TestNewPasswordHasher_RejectsInvalidCost(t *testing.T) {
	tests := []struct {
		name string
		cost int
	}{
		{"cost below bcrypt.MinCost", 1},
		{"cost above bcrypt.MaxCost", 32},
		{"negative cost", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := security.Options{Password: security.PasswordOptions{Cost: tt.cost}}

			_, err := security.NewPasswordHasher(opts)

			if !errors.Is(err, security.ErrInvalidPasswordCost) {
				t.Errorf("NewPasswordHasher(cost %d) error = %v, want ErrInvalidPasswordCost", tt.cost, err)
			}
		})
	}
}
