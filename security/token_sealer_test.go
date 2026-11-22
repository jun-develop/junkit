package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: token_sealer_test.go
// Purpose: Verify sealing (encrypt + sign) and opening of tokens, and key validation.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 13
//
// #   Test Case                                  Scenario                           Expected Result
// 1   TestTokenSealer_OpenReturnsSealedPayload   Seal then open a token             Token is v1.<ciphertext>.<signature>, hides the payload, opens to it
// 2   TestTokenSealer_SealUsesFreshNonce         Seal the same payload twice        The two tokens differ
// 3   TestTokenSealer_RejectsTamperedToken       Ciphertext changed                 Returns an error
// 4   TestTokenSealer_RejectsTamperedToken       Signature changed                  Returns an error
// 5   TestTokenSealer_RejectsTamperedToken       Unknown version prefix             Returns an error
// 6   TestTokenSealer_RejectsTamperedToken       Extra segment appended             Returns an error
// 7   TestTokenSealer_RejectsTamperedToken       Empty token                        Returns an error
// 8   TestTokenSealer_RejectsTamperedToken       Raw token hash instead of a token  Returns an error
// 9   TestTokenSealer_RejectsTamperedToken       5000-character token               Returns an error
// 10  TestTokenSealer_RejectsTokenFromOtherKeys  Token sealed with different keys   Returns ErrBadSignature
// 11  TestNewTokenSealer_RejectsInvalidKeys      No keys                            Returns ErrInvalidKey
// 12  TestNewTokenSealer_RejectsInvalidKeys      HMAC secret too short              Returns ErrInvalidKey
// 13  TestNewTokenSealer_RejectsInvalidKeys      16-byte (AES-128) encryption key   Returns ErrInvalidKey
//
// Fuzz tests: 1
//   FuzzTokenSealer_Open  Random token strings  Never panics; never opens a forged token
//
// =============================================================================

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/security"
)

// testOptions returns valid token keys whose bytes are all fill (and fill+1 for the encryption key).
func testOptions(fill byte) security.Options {
	return security.Options{
		TokenHMACSecret:    bytes.Repeat([]byte{fill}, security.MinHMACSecretSize),
		TokenEncryptionKey: bytes.Repeat([]byte{fill + 1}, security.EncryptionKeySize),
	}
}

// newSealer returns a TokenSealer keyed by testOptions(fill).
func newSealer(t testing.TB, fill byte) security.TokenSealer {
	t.Helper()
	s, err := security.NewTokenSealer(testOptions(fill))
	if err != nil {
		t.Fatalf("NewTokenSealer() error = %v", err)
	}

	return s
}

// seal seals payload and fails the test on error.
func seal(t testing.TB, s security.TokenSealer, payload string) string {
	t.Helper()
	token, err := s.Seal(payload)
	if err != nil {
		t.Fatalf("Seal(%q) error = %v", payload, err)
	}

	return token
}

func TestTokenSealer_OpenReturnsSealedPayload(t *testing.T) {
	s := newSealer(t, 1)
	raw := security.GenerateToken()

	token := seal(t, s, raw)

	if !strings.HasPrefix(token, "v1.") || strings.Count(token, ".") != 2 {
		t.Errorf("Seal() = %q, want v1.<ciphertext>.<signature>", token)
	}

	if strings.Contains(token, raw) {
		t.Error("Seal() result contains the raw payload")
	}

	got, err := s.Open(token)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if got != raw {
		t.Errorf("Open() = %q, want %q", got, raw)
	}
}

func TestTokenSealer_SealUsesFreshNonce(t *testing.T) {
	s := newSealer(t, 1)

	first := seal(t, s, "same")
	second := seal(t, s, "same")

	if first == second {
		t.Error("two seals of the same payload are identical, want a fresh random nonce each time")
	}
}

func TestTokenSealer_RejectsTamperedToken(t *testing.T) {
	s := newSealer(t, 1)
	token := seal(t, s, "payload")
	parts := strings.Split(token, ".")

	// flipMiddle changes one character in the middle of a segment.
	flipMiddle := func(segment string) string {
		b := []byte(segment)
		if b[len(b)/2] == 'A' {
			b[len(b)/2] = 'B'
		} else {
			b[len(b)/2] = 'A'
		}

		return string(b)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"ciphertext changed", strings.Join([]string{parts[0], flipMiddle(parts[1]), parts[2]}, ".")},
		{"signature changed", strings.Join([]string{parts[0], parts[1], flipMiddle(parts[2])}, ".")},
		{"unknown version prefix", strings.Join([]string{"v2", parts[1], parts[2]}, ".")},
		{"extra segment appended", token + ".x"},
		{"empty token", ""},
		{"raw token hash instead of a token", security.HashToken("payload")},
		{"5000-character token", strings.Repeat("a", 5000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Open(tt.token); err == nil {
				t.Error("Open() error = nil, want an error")
			}
		})
	}
}

func TestTokenSealer_RejectsTokenFromOtherKeys(t *testing.T) {
	token := seal(t, newSealer(t, 1), "payload")

	_, err := newSealer(t, 9).Open(token)

	if !errors.Is(err, security.ErrBadSignature) {
		t.Errorf("Open() error = %v, want ErrBadSignature (old tokens must die when keys change)", err)
	}
}

func TestNewTokenSealer_RejectsInvalidKeys(t *testing.T) {
	shortHMAC := testOptions(1)
	shortHMAC.TokenHMACSecret = []byte("short")
	aes128 := testOptions(1)
	aes128.TokenEncryptionKey = make([]byte, 16)

	tests := []struct {
		name string
		opts security.Options
	}{
		{"no keys", security.Options{}},
		{"hmac secret too short", shortHMAC},
		{"16-byte aes-128 encryption key", aes128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := security.NewTokenSealer(tt.opts); !errors.Is(err, security.ErrInvalidKey) {
				t.Errorf("NewTokenSealer() error = %v, want ErrInvalidKey", err)
			}
		})
	}
}

func FuzzTokenSealer_Open(f *testing.F) {
	s := newSealer(f, 1)
	valid := seal(f, s, "seed")

	f.Add(valid)
	f.Add("v1..")
	f.Add("v1.AAAA.AAAA")
	f.Add("...")
	f.Add("v1")

	f.Fuzz(func(t *testing.T, token string) {
		payload, err := s.Open(token)
		if err == nil && token != valid && payload != "seed" {
			t.Errorf("Open(%q) = %q, want an error for a forged token", token, payload)
		}
	})
}
