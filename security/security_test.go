package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: security_test.go
// Purpose: Verify random byte generation and base64 key decoding.
// Type: Unit
//
// Test functions: 3
// Total scenarios: 6
//
// #  Test Case                                 Scenario                   Expected Result
// 1  TestRandomBytes_ReturnsDistinctBytes      Two calls for 32 bytes     Both are 32 bytes long and differ
// 2  TestDecodeKey_AcceptsBothBase64Alphabets  Standard base64, padded    Returns the decoded key
// 3  TestDecodeKey_AcceptsBothBase64Alphabets  URL-safe base64, unpadded  Returns the decoded key
// 4  TestDecodeKey_RejectsInvalidInput         Not base64                 Returns an error
// 5  TestDecodeKey_RejectsInvalidInput         Empty string               Returns an error
// 6  TestDecodeKey_RejectsInvalidInput         Only spaces                Returns an error
//
// =============================================================================

import (
	"bytes"
	"testing"

	"github.com/jun-develop/junkit/security"
)

func TestRandomBytes_ReturnsDistinctBytes(t *testing.T) {
	first := security.RandomBytes(32)
	second := security.RandomBytes(32)

	if len(first) != 32 {
		t.Fatalf("len(first call) = %d, want 32", len(first))
	}

	if len(second) != 32 {
		t.Fatalf("len(second call) = %d, want 32", len(second))
	}

	if bytes.Equal(first, second) {
		t.Error("two calls returned the same bytes, want different random bytes")
	}
}

func TestDecodeKey_AcceptsBothBase64Alphabets(t *testing.T) {
	want := bytes.Repeat([]byte{0xfb}, 32)
	tests := []struct {
		name  string
		input string
	}{
		{"standard base64, padded", "+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/s="},
		{"url-safe base64, unpadded", "-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := security.DecodeKey(tt.input)

			if err != nil {
				t.Fatalf("DecodeKey(%q) error = %v, want nil", tt.input, err)
			}

			if !bytes.Equal(got, want) {
				t.Errorf("DecodeKey(%q) = %x, want %x", tt.input, got, want)
			}
		})
	}
}

func TestDecodeKey_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not base64", "not base64!!"},
		{"empty string", ""},
		{"only spaces", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := security.DecodeKey(tt.input); err == nil {
				t.Errorf("DecodeKey(%q) error = nil, want an error", tt.input)
			}
		})
	}
}
