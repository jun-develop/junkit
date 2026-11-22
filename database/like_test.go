package database_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: like_test.go
// Purpose: Verify that EscapeLike escapes LIKE wildcards and the escape character.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 5
//
// #  Test Case                        Scenario                         Expected Result
// 1  TestEscapeLike_EscapesWildcards  Text without special characters  Returned unchanged
// 2  TestEscapeLike_EscapesWildcards  Percent sign                     Escaped as \%
// 3  TestEscapeLike_EscapesWildcards  Underscore                       Escaped as \_
// 4  TestEscapeLike_EscapesWildcards  Backslash                        Escaped as \\
// 5  TestEscapeLike_EscapesWildcards  Backslash followed by percent    Both escaped: \\\%
//
// =============================================================================

import (
	"testing"

	"github.com/jun-develop/junkit/database"
)

func TestEscapeLike_EscapesWildcards(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"text without special characters", "plain", "plain"},
		{"percent sign", "50%", `50\%`},
		{"underscore", "a_b", `a\_b`},
		{"backslash", `c:\dir`, `c:\\dir`},
		{"backslash followed by percent", `\%`, `\\\%`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := database.EscapeLike(tt.input); got != tt.want {
				t.Errorf("EscapeLike(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
