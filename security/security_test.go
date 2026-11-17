package security_test

import (
	"bytes"
	"testing"

	"github.com/jun-develop/junkit/security"
)

// TestRandomBytes checks length and randomness.
func TestRandomBytes(t *testing.T) {
	a, b := security.RandomBytes(32), security.RandomBytes(32)
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("lengths = %d, %d; want 32", len(a), len(b))
	}

	if bytes.Equal(a, b) {
		t.Error("two calls returned the same bytes")
	}
}

// TestDecodeKey checks both base64 alphabets and invalid input.
func TestDecodeKey(t *testing.T) {
	want := bytes.Repeat([]byte{0xfb}, 32)
	for _, in := range []string{
		"+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/v7+/s=", // std, padded
		"-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_v7-_s",  // url, raw
	} {
		got, err := security.DecodeKey(in)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("DecodeKey(%q) = %x, %v", in, got, err)
		}
	}

	for _, bad := range []string{"not base64!!", "", "   "} {
		if _, err := security.DecodeKey(bad); err == nil {
			t.Errorf("DecodeKey(%q) succeeded", bad)
		}
	}
}
