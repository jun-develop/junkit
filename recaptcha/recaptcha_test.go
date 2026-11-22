package recaptcha_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: recaptcha_test.go
// Purpose: Verify the public constructors: the real client and the always-accepting mock verifier.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 3
//
// #  Test Case                         Scenario                        Expected Result
// 1  TestNewClient_ReturnsClient       Secret key and nil HTTP client  Returns a non-nil client
// 2  TestMockVerifier_AcceptsAnyToken  Token and remote IP             Returns nil
// 3  TestMockVerifier_AcceptsAnyToken  Empty token and remote IP       Returns nil
//
// =============================================================================

import (
	"context"
	"testing"

	"github.com/jun-develop/junkit/recaptcha"
)

func TestNewClient_ReturnsClient(t *testing.T) {
	if recaptcha.NewClient(recaptcha.Options{SecretKey: "x"}, nil) == nil {
		t.Fatal("NewClient() = nil, want a client")
	}
}

func TestMockVerifier_AcceptsAnyToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		remoteIP string
	}{
		{"token and remote ip", "any-token", "1.2.3.4"},
		{"empty token and remote ip", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := recaptcha.NewMockVerifier()

			if err := v.Verify(context.Background(), tt.token, tt.remoteIP); err != nil {
				t.Fatalf("Verify(%q, %q) error = %v, want nil", tt.token, tt.remoteIP, err)
			}
		})
	}
}
