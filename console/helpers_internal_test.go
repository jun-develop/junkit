package console

import "testing"

func TestEnsureUpper(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"đẹp trai", "Đẹp trai"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ensureUpper(tt.in); got != tt.want {
				t.Errorf("ensureUpper(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnsureLower(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"Hello", "hello"},
		{"hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ensureLower(tt.in); got != tt.want {
				t.Errorf("ensureLower(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
