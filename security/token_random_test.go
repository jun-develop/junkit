package security_test

import (
	"encoding/base64"
	"testing"

	"github.com/jun-develop/junkit/security"
)

// TestGenerateToken checks token encoding, size and uniqueness.
func TestGenerateToken(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for range 1000 {
		tok := security.GenerateToken()
		b, err := base64.RawURLEncoding.DecodeString(tok)
		if err != nil {
			t.Fatalf("token %q is not raw base64url: %v", tok, err)
		}

		if len(b) != security.TokenSize {
			t.Fatalf("decoded length = %d, want %d", len(b), security.TokenSize)
		}

		if seen[tok] {
			t.Fatalf("duplicate token %q", tok)
		}

		seen[tok] = true
	}
}
