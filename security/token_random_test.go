package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: token_random_test.go
// Purpose: Verify the encoding, size and uniqueness of generated tokens.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 1
//
// #  Test Case                              Scenario              Expected Result
// 1  TestGenerateToken_ReturnsUniqueTokens  Generate 1000 tokens  Each is raw base64url of TokenSize bytes; none repeat
//
// =============================================================================

import (
	"encoding/base64"
	"testing"

	"github.com/jun-develop/junkit/security"
)

func TestGenerateToken_ReturnsUniqueTokens(t *testing.T) {
	seen := make(map[string]bool, 1000)

	for range 1000 {
		token := security.GenerateToken()

		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil {
			t.Fatalf("token %q is not raw base64url: %v", token, err)
		}

		if len(decoded) != security.TokenSize {
			t.Fatalf("decoded token length = %d, want %d", len(decoded), security.TokenSize)
		}

		if seen[token] {
			t.Fatalf("token %q was generated twice", token)
		}

		seen[token] = true
	}
}
