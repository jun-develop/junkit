package database_test

import (
	"testing"

	"github.com/jun-develop/junkit/database"
)

func TestEscapeLike_EscapesWildcards(t *testing.T) {
	tests := map[string]string{
		"plain":  "plain",
		"50%":    `50\%`,
		"a_b":    `a\_b`,
		`c:\dir`: `c:\\dir`,
		`\%`:     `\\\%`,
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			if got := database.EscapeLike(in); got != want {
				t.Errorf("EscapeLike(%q) = %q, want %q", in, got, want)
			}
		})
	}
}
