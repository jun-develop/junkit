package security_test

import (
	"testing"

	"github.com/jun-develop/junkit/security"
)

func TestClientKey(t *testing.T) {
	tests := []struct{ ip, want string }{
		{"203.0.113.7", "203.0.113.7"},
		{"2001:db8:1:2:aaaa::1", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:bbbb:cccc:dddd:eeee", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"fe80::1%eth0", "fe80::/64"},
		{"::ffff:203.0.113.7", "203.0.113.7"},
		{"not-an-ip", "not-an-ip"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := security.ClientKey(tt.ip); got != tt.want {
			t.Errorf("ClientKey(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}
