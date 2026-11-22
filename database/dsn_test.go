package database_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: dsn_test.go
// Purpose: Verify that MaskDSN hides secrets in key=value and URL connection strings.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 12
//
// #   Test Case                   Scenario                              Expected Result
// 1   TestMaskDSN_RedactsSecrets  Empty string                          Returned unchanged
// 2   TestMaskDSN_RedactsSecrets  password=... key                      Value replaced by ********
// 3   TestMaskDSN_RedactsSecrets  Upper-case PASSWORD key               Value masked (case-insensitive)
// 4   TestMaskDSN_RedactsSecrets  pwd, secret, token, api_key, api-key  All values masked
// 5   TestMaskDSN_RedactsSecrets  Single-quoted value with spaces       Whole quoted value masked
// 6   TestMaskDSN_RedactsSecrets  No secret keys                        Returned unchanged
// 7   TestMaskDSN_RedactsSecrets  Escaped quote inside a quoted value   Whole quoted value masked
// 8   TestMaskDSN_RedactsSecrets  Double-quoted value                   Whole quoted value masked
// 9   TestMaskDSN_RedactsSecrets  URL with user:password                Password masked
// 10  TestMaskDSN_RedactsSecrets  URL password containing @             Whole password masked
// 11  TestMaskDSN_RedactsSecrets  URL without a password                Returned unchanged
// 12  TestMaskDSN_RedactsSecrets  URL query parameter password          Masked, along with every parameter after it
//
// =============================================================================

import (
	"testing"

	"github.com/jun-develop/junkit/database"
)

func TestMaskDSN_RedactsSecrets(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{name: "empty string is returned unchanged", dsn: "", want: ""},
		{
			name: "password is masked",
			dsn:  "host=localhost port=5432 user=postgres password=secret123 dbname=app",
			want: "host=localhost port=5432 user=postgres password=******** dbname=app",
		},
		{
			name: "key match is case-insensitive",
			dsn:  "user=admin PASSWORD=Secret123",
			want: "user=admin PASSWORD=********",
		},
		{
			name: "pwd, secret, token, and api_key are all recognized",
			dsn:  "pwd=a secret=b token=c api_key=d api-key=e",
			want: "pwd=******** secret=******** token=******** api_key=******** api-key=********",
		},
		{
			name: "single-quoted values are masked whole",
			dsn:  "password='p@ss word' host=localhost",
			want: "password=******** host=localhost",
		},
		{
			name: "a DSN with no recognized secret keys is unchanged",
			dsn:  "host=localhost port=5432 dbname=app sslmode=disable",
			want: "host=localhost port=5432 dbname=app sslmode=disable",
		},
		{
			name: "an escaped quote inside a quoted value does not end it",
			dsn:  `password='ab\'cd ef' host=localhost`,
			want: "password=******** host=localhost",
		},
		{
			name: "double-quoted values are masked whole",
			dsn:  `password="p@ss \" word" host=localhost`,
			want: "password=******** host=localhost",
		},
		{
			name: "URL password is masked",
			dsn:  "postgres://user:pass@localhost:5432/app",
			want: "postgres://user:********@localhost:5432/app",
		},
		{
			name: "URL password containing @ is masked whole",
			dsn:  "postgres://user:p@ss@localhost/app?sslmode=disable",
			want: "postgres://user:********@localhost/app?sslmode=disable",
		},
		{
			name: "URL without a password is unchanged",
			dsn:  "postgres://user@localhost/app",
			want: "postgres://user@localhost/app",
		},
		{
			name: "URL query parameter password is masked, over-masking the parameters after it",
			dsn:  "postgres://localhost/app?user=u&password=secret&sslmode=disable",
			want: "postgres://localhost/app?user=u&password=********",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := database.MaskDSN(tt.dsn); got != tt.want {
				t.Fatalf("MaskDSN(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}
