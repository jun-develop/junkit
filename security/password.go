package security

import (
	"runtime"

	"golang.org/x/crypto/bcrypt"
)

// maxPasswordBytes is bcrypt's input limit.
const maxPasswordBytes = 72

// PasswordHasher hashes and verifies passwords with bcrypt. Hash returns ErrPasswordTooLong for
// input over 72 bytes instead of silently truncating; Compare reports whether plain matches hash;
// CompareDummy spends the same time as Compare, so call it when the user does not exist to avoid
// leaking account existence through timing.
type PasswordHasher interface {
	Hash(plain string) (string, error)
	Compare(hash, plain string) bool
	CompareDummy(plain string)
}

// passwordHasher is the default bcrypt PasswordHasher.
type passwordHasher struct {
	cost      int
	dummyHash []byte
	slots     chan struct{}
}

// NewPasswordHasher creates a PasswordHasher with opts.Password.Cost.
//
// Usage:
//
//	hasher, err := security.NewPasswordHasher(security.Options{
//	    Password: security.PasswordOptions{Cost: 12},
//	})
//	hash, err := hasher.Hash("s3cret-pass")
//	ok := hasher.Compare(hash, "s3cret-pass")
func NewPasswordHasher(opts Options) (PasswordHasher, error) {
	cost := opts.Password.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}

	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return nil, ErrInvalidPasswordCost
	}

	dummy, err := bcrypt.GenerateFromPassword([]byte("security.dummy-password"), cost)
	if err != nil {
		return nil, err
	}

	limit := opts.Password.MaxConcurrent
	if limit <= 0 {
		limit = max(1, runtime.GOMAXPROCS(0)/2)
	}

	return &passwordHasher{cost: cost, dummyHash: dummy, slots: make(chan struct{}, limit)}, nil
}

// acquire waits for a free slot and returns its release.
func (h *passwordHasher) acquire() func() {
	h.slots <- struct{}{}
	return func() { <-h.slots }
}

// Hash implements PasswordHasher; passwords over 72 bytes return ErrPasswordTooLong.
func (h *passwordHasher) Hash(plain string) (string, error) {
	if len(plain) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}

	defer h.acquire()()

	b, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// Compare implements PasswordHasher.
func (h *passwordHasher) Compare(hash, plain string) bool {
	if len(plain) > maxPasswordBytes {
		h.CompareDummy(plain)
		return false
	}

	defer h.acquire()()

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// CompareDummy implements PasswordHasher.
func (h *passwordHasher) CompareDummy(plain string) {
	if len(plain) > maxPasswordBytes {
		plain = plain[:maxPasswordBytes]
	}

	defer h.acquire()()

	_ = bcrypt.CompareHashAndPassword(h.dummyHash, []byte(plain))
}
