package stringx_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: stringx_test.go
// Purpose: Verify the case conversions between Go identifiers, snake_case and first-letter casing.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 22
//
// #   Test Case                              Scenario                               Expected Result
// 1   TestToSnake_ConvertsIdentifiers        Two words ("UserName")                 "user_name"
// 2   TestToSnake_ConvertsIdentifiers        Already lowercase                      Unchanged
// 3   TestToSnake_ConvertsIdentifiers        Trailing acronym ("UserID")            "user_id"
// 4   TestToSnake_ConvertsIdentifiers        Acronym followed by a word ("APIKey")  "api_key"
// 5   TestToSnake_ConvertsIdentifiers        Acronym at the end ("AvatarURL")       "avatar_url"
// 6   TestToSnake_ConvertsIdentifiers        Long PascalCase                        "create_user_request"
// 7   TestToSnake_ConvertsIdentifiers        Digit then a word ("Field2Name")       "field2_name"
// 8   TestToSnake_ConvertsIdentifiers        Digit then an acronym ("User2FA")      "user2_fa"
// 9   TestToSnake_ConvertsIdentifiers        Empty string                           Empty string
// 10  TestToPascal_ConvertsSnakeCase         Two words                              "OrderItem"
// 11  TestToPascal_ConvertsSnakeCase         One word                               "Order"
// 12  TestToPascal_ConvertsSnakeCase         Doubled underscore                     Empty part skipped
// 13  TestToPascal_ConvertsSnakeCase         Empty string                           Empty string
// 14  TestToCamel_ConvertsSnakeCase          Two words                              "orderItem"
// 15  TestToCamel_ConvertsSnakeCase          Empty string                           Empty string
// 16  TestUpperFirst_CapitalizesFirstLetter  Empty string                           Empty string
// 17  TestUpperFirst_CapitalizesFirstLetter  Lower-case first letter                First letter upper-cased
// 18  TestUpperFirst_CapitalizesFirstLetter  Already capitalized                    Unchanged
// 19  TestUpperFirst_CapitalizesFirstLetter  Multi-byte (Vietnamese) first letter   "Đẹp trai"
// 20  TestLowerFirst_LowercasesFirstLetter   Empty string                           Empty string
// 21  TestLowerFirst_LowercasesFirstLetter   Upper-case first letter                First letter lower-cased
// 22  TestLowerFirst_LowercasesFirstLetter   Already lower-case                     Unchanged
//
// =============================================================================

import (
	"testing"

	"github.com/jun-develop/junkit/stringx"
)

type caseTest struct {
	name string
	in   string
	want string
}

func run(t *testing.T, fnName string, fn func(string) string, tests []caseTest) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fn(tt.in); got != tt.want {
				t.Fatalf("%s(%q) = %q, want %q", fnName, tt.in, got, tt.want)
			}
		})
	}
}

func TestToSnake_ConvertsIdentifiers(t *testing.T) {
	run(t, "ToSnake", stringx.ToSnake, []caseTest{
		{name: "two words", in: "UserName", want: "user_name"},
		{name: "already lowercase", in: "email", want: "email"},
		{name: "trailing acronym", in: "UserID", want: "user_id"},
		{name: "acronym followed by a word", in: "APIKey", want: "api_key"},
		{name: "acronym at the end", in: "AvatarURL", want: "avatar_url"},
		{name: "long PascalCase", in: "CreateUserRequest", want: "create_user_request"},
		{name: "digit then a word", in: "Field2Name", want: "field2_name"},
		{name: "digit then an acronym", in: "User2FA", want: "user2_fa"},
		{name: "empty string", in: "", want: ""},
	})
}

func TestToPascal_ConvertsSnakeCase(t *testing.T) {
	run(t, "ToPascal", stringx.ToPascal, []caseTest{
		{name: "two words", in: "order_item", want: "OrderItem"},
		{name: "one word", in: "order", want: "Order"},
		{name: "doubled underscore", in: "order__item", want: "OrderItem"},
		{name: "empty string", in: "", want: ""},
	})
}

func TestToCamel_ConvertsSnakeCase(t *testing.T) {
	run(t, "ToCamel", stringx.ToCamel, []caseTest{
		{name: "two words", in: "order_item", want: "orderItem"},
		{name: "empty string", in: "", want: ""},
	})
}

func TestUpperFirst_CapitalizesFirstLetter(t *testing.T) {
	run(t, "UpperFirst", stringx.UpperFirst, []caseTest{
		{name: "empty string", in: "", want: ""},
		{name: "lower-case first letter", in: "hello", want: "Hello"},
		{name: "already capitalized", in: "Hello", want: "Hello"},
		{name: "multi-byte first letter", in: "đẹp trai", want: "Đẹp trai"},
	})
}

func TestLowerFirst_LowercasesFirstLetter(t *testing.T) {
	run(t, "LowerFirst", stringx.LowerFirst, []caseTest{
		{name: "empty string", in: "", want: ""},
		{name: "upper-case first letter", in: "Hello", want: "hello"},
		{name: "already lower-case", in: "hello", want: "hello"},
	})
}
