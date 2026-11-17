package recaptcha

import "context"

// MockVerifier is a Verifier that accepts every token unconditionally.
type MockVerifier struct{}

// NewMockVerifier creates a MockVerifier that accepts every token unconditionally.
func NewMockVerifier() *MockVerifier {
	return &MockVerifier{}
}

// Verify implements Verifier; it always returns nil.
func (m *MockVerifier) Verify(context.Context, string, string) error {
	return nil
}
