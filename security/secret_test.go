package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: secret_test.go
// Purpose: Verify detection of secret-looking names and masking of secret header values.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 29
//
// #   Test Case                      Scenario                        Expected Result
// 1   TestIsSecretName_MatchesHints  "password"                      true
// 2   TestIsSecretName_MatchesHints  "Password_Confirmation"         true (case-insensitive substring)
// 3   TestIsSecretName_MatchesHints  "passwd"                        true
// 4   TestIsSecretName_MatchesHints  "PWD"                           true
// 5   TestIsSecretName_MatchesHints  "client_secret"                 true
// 6   TestIsSecretName_MatchesHints  "reset_token"                   true
// 7   TestIsSecretName_MatchesHints  "X-Api-Key"                     true
// 8   TestIsSecretName_MatchesHints  "api_key"                       true
// 9   TestIsSecretName_MatchesHints  "apikey"                        true
// 10  TestIsSecretName_MatchesHints  "Authorization"                 true
// 11  TestIsSecretName_MatchesHints  "X-Hub-Signature"               true
// 12  TestIsSecretName_MatchesHints  "session_id"                    true
// 13  TestIsSecretName_MatchesHints  "Set-Cookie"                    true
// 14  TestIsSecretName_MatchesHints  "username"                      false
// 15  TestIsSecretName_MatchesHints  "email"                         false
// 16  TestIsSecretName_MatchesHints  "Content-Type"                  false
// 17  TestIsSecretName_MatchesHints  Empty name                      false
// 18  TestMaskHeader_HidesSecrets    Bearer authorization            Keeps the scheme: "Bearer ****"
// 19  TestMaskHeader_HidesSecrets    Basic authorization             Keeps the scheme: "Basic ****"
// 20  TestMaskHeader_HidesSecrets    Proxy-Authorization             Keeps the scheme: "Bearer ****"
// 21  TestMaskHeader_HidesSecrets    Authorization without a scheme  "****"
// 22  TestMaskHeader_HidesSecrets    Empty authorization             Empty string
// 23  TestMaskHeader_HidesSecrets    Cookie with two values          Keeps cookie names, masks values
// 24  TestMaskHeader_HidesSecrets    Set-Cookie                      Keeps the cookie name, masks the value
// 25  TestMaskHeader_HidesSecrets    Empty cookie                    Empty string
// 26  TestMaskHeader_HidesSecrets    X-Api-Key header                "****"
// 27  TestMaskHeader_HidesSecrets    X-Auth-Token header             "****"
// 28  TestMaskHeader_HidesSecrets    X-Session-Id header             "****"
// 29  TestMaskHeader_HidesSecrets    Content-Type header             Value unchanged
//
// =============================================================================

import (
	"strconv"
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

	// The quoted input is the subtest name: it is the whole scenario.
	for _, tt := range tests {
		t.Run(strconv.Quote(tt.name), func(t *testing.T) {
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
