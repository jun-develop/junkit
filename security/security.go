// Package security provides token sealing, token hashing, password hashing, secret-name
// detection and identity context helpers.
//
// Usage:
//
//	fx.New(
//	    fx.Supply(security.Options{TokenHMACSecret: hmacKey, TokenEncryptionKey: encKey}),
//	    security.Module,
//	)
package security

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// Key length requirements.
const (
	// MinHMACSecretSize is the minimum HMAC secret length in bytes.
	MinHMACSecretSize = 32
	// EncryptionKeySize is the AES-256 key length in bytes.
	EncryptionKeySize = 32
)

// PasswordOptions configures bcrypt hashing.
type PasswordOptions struct {
	// Cost is the bcrypt cost (4-31, defaults to bcrypt.DefaultCost when 0).
	Cost int

	// MaxConcurrent caps how many hashes and compares run at once; the rest wait their turn, so a
	// login flood cannot take every core from the other routes. Defaults to half of GOMAXPROCS
	// (at least 1) when <= 0.
	MaxConcurrent int
}

// Options configures the security package.
type Options struct {
	// TokenHMACSecret signs tokens (at least MinHMACSecretSize bytes).
	TokenHMACSecret []byte

	// TokenEncryptionKey encrypts tokens with AES-256-GCM (exactly EncryptionKeySize bytes).
	TokenEncryptionKey []byte

	// Password configures PasswordHasher.
	Password PasswordOptions
}

// validateTokenKeys checks the token key lengths.
func (o Options) validateTokenKeys() error {
	if len(o.TokenHMACSecret) < MinHMACSecretSize {
		return fmt.Errorf("%w: hmac secret must be at least %d bytes", ErrInvalidKey, MinHMACSecretSize)
	}

	if len(o.TokenEncryptionKey) != EncryptionKeySize {
		return fmt.Errorf("%w: encryption key must be exactly %d bytes", ErrInvalidKey, EncryptionKeySize)
	}

	return nil
}

// RandomBytes returns n bytes from crypto/rand.
//
// Usage:
//
//	secret := security.RandomBytes(security.MinHMACSecretSize)
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	// crypto/rand.Read never returns an error (it crashes the program instead).
	_, _ = rand.Read(b)

	return b
}

// DecodeKey decodes a base64 key (standard or URL alphabet, with or without padding).
//
// Usage:
//
//	secret, err := security.DecodeKey(os.Getenv("AUTH_TOKEN_HMAC_SECRET"))
func DecodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidKey)
	}

	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}

	return nil, fmt.Errorf("%w: not valid base64", ErrInvalidKey)
}
