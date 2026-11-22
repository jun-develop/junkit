package console

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: helpers_internal_test.go
// Purpose: Verify the helpers that upper-case or lower-case the first letter of a message.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 7
//
// #  Test Case                               Scenario                              Expected Result
// 1  TestEnsureUpper_CapitalizesFirstLetter  Empty string                          Returns an empty string
// 2  TestEnsureUpper_CapitalizesFirstLetter  Lower-case first letter               First letter upper-cased
// 3  TestEnsureUpper_CapitalizesFirstLetter  Already capitalized                   Returned unchanged
// 4  TestEnsureUpper_CapitalizesFirstLetter  Multi-byte (Vietnamese) first letter  First letter upper-cased ("Đẹp trai")
// 5  TestEnsureLower_LowercasesFirstLetter   Empty string                          Returns an empty string
// 6  TestEnsureLower_LowercasesFirstLetter   Upper-case first letter               First letter lower-cased
// 7  TestEnsureLower_LowercasesFirstLetter   Already lower-case                    Returned unchanged
//
// =============================================================================

import "testing"

func TestEnsureUpper_CapitalizesFirstLetter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"lower-case first letter", "hello", "Hello"},
		{"already capitalized", "Hello", "Hello"},
		{"multi-byte first letter", "đẹp trai", "Đẹp trai"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ensureUpper(tt.input); got != tt.want {
				t.Errorf("ensureUpper(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEnsureLower_LowercasesFirstLetter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"upper-case first letter", "Hello", "hello"},
		{"already lower-case", "hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ensureLower(tt.input); got != tt.want {
				t.Errorf("ensureLower(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
