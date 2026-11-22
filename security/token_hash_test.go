package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: token_hash_test.go
// Purpose: Verify token hashing and constant-time secret comparison.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 7
//
// #  Test Case                                Scenario                 Expected Result
// 1  TestHashToken_ReturnsSHA256Hex           Known input "abc"        Returns the known SHA-256 hex digest
// 2  TestHashToken_ReturnsSHA256Hex           Freshly generated token  Returns 64 lowercase hex characters
// 3  TestSecureEqual_TrueOnlyForEqualSecrets  Identical strings        Returns true
// 4  TestSecureEqual_TrueOnlyForEqualSecrets  Different letter case    Returns false
// 5  TestSecureEqual_TrueOnlyForEqualSecrets  Longer string            Returns false
// 6  TestSecureEqual_TrueOnlyForEqualSecrets  Empty string             Returns false
// 7  TestSecureEqual_TrueOnlyForEqualSecrets  Shorter prefix           Returns false
//
// =============================================================================

import (
	"regexp"
	"testing"

	"github.com/jun-develop/junkit/security"
)

func TestHashToken_ReturnsSHA256Hex(t *testing.T) {
	t.Run("known input abc", func(t *testing.T) {
		const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

		if got := security.HashToken("abc"); got != want {
			t.Errorf("HashToken(abc) = %s, want %s", got, want)
		}
	})

	t.Run("freshly generated token", func(t *testing.T) {
		got := security.HashToken(security.GenerateToken())

		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) {
			t.Errorf("HashToken() = %q, want 64 lowercase hex characters", got)
		}
	})
}

func TestSecureEqual_TrueOnlyForEqualSecrets(t *testing.T) {
	tests := []struct {
		name  string
		other string
		want  bool
	}{
		{"identical strings", "secret", true},
		{"different letter case", "Secret", false},
		{"longer string", "secret2", false},
		{"empty string", "", false},
		{"shorter prefix", "secre", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := security.SecureEqual("secret", tt.other); got != tt.want {
				t.Errorf("SecureEqual(secret, %q) = %v, want %v", tt.other, got, tt.want)
			}
		})
	}
}
