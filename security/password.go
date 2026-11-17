package security

import (
	"context"
	"fmt"
	"runtime"

	"golang.org/x/crypto/bcrypt"
)

// maxPasswordBytes is bcrypt's input limit.
const maxPasswordBytes = 72

// PasswordHasher hashes and verifies passwords with bcrypt. Every method first waits for a free
// hashing slot and returns ctx's error, wrapped, if ctx ends first. Hash returns
// ErrPasswordTooLong for input over 72 bytes instead of silently truncating; Compare reports
// whether plain matches hash; CompareDummy spends the same time as Compare, so call it when the
// user does not exist to avoid leaking account existence through timing.
type PasswordHasher interface {
	Hash(ctx context.Context, plain string) (string, error)
	Compare(ctx context.Context, hash, plain string) (bool, error)
	CompareDummy(ctx context.Context, plain string) error
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
//	hash, err := hasher.Hash(ctx, "s3cret-pass")
//	ok, err := hasher.Compare(ctx, hash, "s3cret-pass")
func NewPasswordHasher(opts Options) (PasswordHasher, error) {
	opts = opts.withDefaults()

	cost := opts.Password.Cost
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return nil, ErrInvalidPasswordCost
	}

	dummy, err := bcrypt.GenerateFromPassword([]byte("security.dummy-password"), cost)
	if err != nil {
		return nil, fmt.Errorf("security: dummy hash: %w", err)
	}

	slots := make(chan struct{}, opts.Password.MaxConcurrent)

	return &passwordHasher{cost: cost, dummyHash: dummy, slots: slots}, nil
}

// acquire waits for a free slot or for ctx to end, and returns the slot's release.
func (h *passwordHasher) acquire(ctx context.Context) (func(), error) {
	select {
	case h.slots <- struct{}{}:
		return func() { <-h.slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("security: wait for hash slot: %w", ctx.Err())
	}
}

// Hash implements PasswordHasher; passwords over 72 bytes return ErrPasswordTooLong.
func (h *passwordHasher) Hash(ctx context.Context, plain string) (string, error) {
	if len(plain) > maxPasswordBytes {
		return "", ErrPasswordTooLong
	}

	release, err := h.acquire(ctx)
	if err != nil {
		return "", err
	}

	defer release()

	b, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", fmt.Errorf("security: hash: %w", err)
	}

	return string(b), nil
}

// Compare implements PasswordHasher; a mismatch or malformed hash is false with a nil error.
func (h *passwordHasher) Compare(ctx context.Context, hash, plain string) (bool, error) {
	if len(plain) > maxPasswordBytes {
		return false, h.CompareDummy(ctx, plain)
	}

	release, err := h.acquire(ctx)
	if err != nil {
		return false, err
	}

	defer release()

	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil, nil
}

// CompareDummy implements PasswordHasher.
func (h *passwordHasher) CompareDummy(ctx context.Context, plain string) error {
	if len(plain) > maxPasswordBytes {
		plain = plain[:maxPasswordBytes]
	}

	release, err := h.acquire(ctx)
	if err != nil {
		return err
	}

	defer release()

	// The result is irrelevant: the call exists only to spend the time a real Compare would.
	_ = bcrypt.CompareHashAndPassword(h.dummyHash, []byte(plain))

	return nil
}

// withDefaults fills unset password options: bcrypt.DefaultCost when Cost is 0, and half of
// GOMAXPROCS (at least 1) when MaxConcurrent <= 0.
func (o Options) withDefaults() Options {
	if o.Password.Cost == 0 {
		o.Password.Cost = bcrypt.DefaultCost
	}

	if o.Password.MaxConcurrent <= 0 {
		o.Password.MaxConcurrent = max(1, runtime.GOMAXPROCS(0)/2)
	}

	return o
}
