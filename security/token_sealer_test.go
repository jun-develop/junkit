package security_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jun-develop/junkit/security"
)

// testOptions returns valid token keys filled with fill.
func testOptions(fill byte) security.Options {
	return security.Options{
		TokenHMACSecret:    bytes.Repeat([]byte{fill}, security.MinHMACSecretSize),
		TokenEncryptionKey: bytes.Repeat([]byte{fill + 1}, security.EncryptionKeySize),
	}
}

// newSealer returns a TokenSealer keyed by fill.
func newSealer(t testing.TB, fill byte) security.TokenSealer {
	t.Helper()
	s, err := security.NewTokenSealer(testOptions(fill))
	if err != nil {
		t.Fatalf("NewTokenSealer: %v", err)
	}

	return s
}

// TestTokenSealer_RoundTrip checks that Open returns what Seal sealed.
func TestTokenSealer_RoundTrip(t *testing.T) {
	s := newSealer(t, 1)
	raw := security.GenerateToken()

	token, err := s.Seal(raw)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if !strings.HasPrefix(token, "v1.") || strings.Count(token, ".") != 2 {
		t.Errorf("token = %q, want v1.<ciphertext>.<signature>", token)
	}

	if strings.Contains(token, raw) {
		t.Error("sealed token contains the raw payload")
	}

	got, err := s.Open(token)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if got != raw {
		t.Errorf("Open = %q, want %q", got, raw)
	}
}

// TestTokenSealer_SealIsRandomized checks that each seal uses a fresh nonce.
func TestTokenSealer_SealIsRandomized(t *testing.T) {
	s := newSealer(t, 1)
	a, _ := s.Seal("same")
	b, _ := s.Seal("same")
	if a == b {
		t.Error("two seals of the same payload are identical; nonce is not random")
	}
}

// TestTokenSealer_TamperedSegmentsRejected checks that altered tokens fail to open.
func TestTokenSealer_TamperedSegmentsRejected(t *testing.T) {
	s := newSealer(t, 1)
	token, _ := s.Seal("payload")
	parts := strings.Split(token, ".")

	flip := func(seg string) string {
		b := []byte(seg)
		if b[len(b)/2] == 'A' {
			b[len(b)/2] = 'B'
		} else {
			b[len(b)/2] = 'A'
		}

		return string(b)
	}

	cases := map[string]string{
		"ciphertext": strings.Join([]string{parts[0], flip(parts[1]), parts[2]}, "."),
		"signature":  strings.Join([]string{parts[0], parts[1], flip(parts[2])}, "."),
		"version":    strings.Join([]string{"v2", parts[1], parts[2]}, "."),
		"extra part": token + ".x",
		"empty":      "",
		"raw hash":   security.HashToken("payload"),
		"too long":   strings.Repeat("a", 5000),
	}
	for name, tampered := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Open(tampered); err == nil {
				t.Errorf("Open(%s) succeeded, want error", name)
			}
		})
	}
}

// TestTokenSealer_OtherSecretRejected checks that a different key rejects a token.
func TestTokenSealer_OtherSecretRejected(t *testing.T) {
	token, _ := newSealer(t, 1).Seal("payload")
	if _, err := newSealer(t, 9).Open(token); !errors.Is(err, security.ErrBadSignature) {
		t.Errorf("err = %v, want ErrBadSignature (old tokens die when keys change)", err)
	}
}

// TestNewTokenSealer_InvalidOptions checks key length validation.
func TestNewTokenSealer_InvalidOptions(t *testing.T) {
	shortHMAC := testOptions(1)
	shortHMAC.TokenHMACSecret = []byte("short")
	aes128 := testOptions(1)
	aes128.TokenEncryptionKey = make([]byte, 16)

	for name, opts := range map[string]security.Options{
		"no keys":     {},
		"short hmac":  shortHMAC,
		"aes-128 key": aes128,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := security.NewTokenSealer(opts); !errors.Is(err, security.ErrInvalidKey) {
				t.Errorf("err = %v, want ErrInvalidKey", err)
			}
		})
	}
}

// FuzzTokenSealerOpen checks that Open never panics or accepts forged input.
func FuzzTokenSealerOpen(f *testing.F) {
	s := newSealer(f, 1)
	valid, _ := s.Seal("seed")

	f.Add(valid)
	f.Add("v1..")
	f.Add("v1.AAAA.AAAA")
	f.Add("...")
	f.Add("v1")

	f.Fuzz(func(t *testing.T, token string) {
		payload, err := s.Open(token)
		if err == nil && token != valid && payload != "seed" {
			t.Errorf("Open accepted forged token %q -> %q", token, payload)
		}
	})
}
