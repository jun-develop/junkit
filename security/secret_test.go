package security_test

import (
	"testing"

	"github.com/jun-develop/junkit/security"
)

// TestIsSecretName_MatchesHints checks the case-insensitive substring match on the shared hints.
func TestIsSecretName_MatchesHints(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"password", true},
		{"Password_Confirmation", true},
		{"passwd", true},
		{"PWD", true},
		{"client_secret", true},
		{"reset_token", true},
		{"X-Api-Key", true},
		{"api_key", true},
		{"apikey", true},
		{"Authorization", true},
		{"X-Hub-Signature", true},
		{"session_id", true},
		{"Set-Cookie", true},
		{"username", false},
		{"email", false},
		{"Content-Type", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := security.IsSecretName(tt.name); got != tt.want {
				t.Errorf("IsSecretName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestMaskHeader_HidesSecrets checks each masking shape MaskHeader applies.
func TestMaskHeader_HidesSecrets(t *testing.T) {
	tests := []struct {
		desc, name, value, want string
	}{
		{"bearer keeps the scheme", "Authorization", "Bearer abcdef", "Bearer ****"},
		{"basic keeps the scheme", "authorization", "Basic dXNlcjpwYXNz", "Basic ****"},
		{"proxy authorization", "Proxy-Authorization", "Bearer x", "Bearer ****"},
		{"bare credential", "Authorization", "raw-api-key", "****"},
		{"empty authorization", "Authorization", "", ""},
		{"cookie keeps names", "Cookie", "session=abc; theme=dark", "session=****; theme=****"},
		{"set-cookie", "Set-Cookie", "sid=abc", "sid=****"},
		{"empty cookie", "Cookie", "", ""},
		{"api key header", "X-Api-Key", "k123", "****"},
		{"token header", "X-Auth-Token", "t123", "****"},
		{"session header", "X-Session-Id", "s123", "****"},
		{"plain header unchanged", "Content-Type", "application/json", "application/json"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := security.MaskHeader(tt.name, tt.value); got != tt.want {
				t.Errorf("MaskHeader(%q, %q) = %q, want %q", tt.name, tt.value, got, tt.want)
			}
		})
	}
}
