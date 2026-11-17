package security

import "errors"

// Errors returned by TokenSealer.Open, the constructors and PasswordHasher.Hash.
var (
	ErrMalformedToken      = errors.New("security: malformed token")
	ErrBadSignature        = errors.New("security: invalid token signature")
	ErrDecrypt             = errors.New("security: token decryption failed")
	ErrInvalidKey          = errors.New("security: invalid token key")
	ErrInvalidPasswordCost = errors.New("security: invalid bcrypt cost")
	ErrPasswordTooLong     = errors.New("security: password exceeds 72 bytes")
)
