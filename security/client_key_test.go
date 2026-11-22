package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: client_key_test.go
// Purpose: Verify that ClientKey turns a client IP into a rate-limit key.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 8
//
// #  Test Case                        Scenario                     Expected Result
// 1  TestClientKey_NormalizesAddress  IPv4 address                 Returned unchanged
// 2  TestClientKey_NormalizesAddress  IPv6 address                 Grouped into its /64 prefix
// 3  TestClientKey_NormalizesAddress  Other host in the same /64   Same /64 key as the first host
// 4  TestClientKey_NormalizesAddress  Host in a different /64      A different /64 key
// 5  TestClientKey_NormalizesAddress  Link-local IPv6 with a zone  Zone dropped, grouped into /64
// 6  TestClientKey_NormalizesAddress  IPv4-mapped IPv6 address     Returned as plain IPv4
// 7  TestClientKey_NormalizesAddress  Not an IP address            Returned unchanged
// 8  TestClientKey_NormalizesAddress  Empty string                 Returns an empty string
//
// =============================================================================

import (
	"testing"

	"github.com/jun-develop/junkit/security"
)

func TestClientKey_NormalizesAddress(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		{"ipv4 address", "203.0.113.7", "203.0.113.7"},
		{"ipv6 address", "2001:db8:1:2:aaaa::1", "2001:db8:1:2::/64"},
		{"other host in the same /64", "2001:db8:1:2:bbbb:cccc:dddd:eeee", "2001:db8:1:2::/64"},
		{"host in a different /64", "2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"link-local ipv6 with a zone", "fe80::1%eth0", "fe80::/64"},
		{"ipv4-mapped ipv6 address", "::ffff:203.0.113.7", "203.0.113.7"},
		{"not an ip address", "not-an-ip", "not-an-ip"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := security.ClientKey(tt.ip); got != tt.want {
				t.Errorf("ClientKey(%q) = %q, want %q", tt.ip, got, tt.want)
			}
		})
	}
}
