package validation

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: helper_internal_test.go
// Purpose: Verify the name helpers that turn Go field paths into snake_case error keys.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 11
//
// #   Test Case                                 Scenario                               Expected Result
// 1   TestCamelToSnake_ConvertsIdentifiers      Two words ("UserName")                 "user_name"
// 2   TestCamelToSnake_ConvertsIdentifiers      Already lowercase                      Unchanged
// 3   TestCamelToSnake_ConvertsIdentifiers      Trailing acronym ("UserID")            "user_id"
// 4   TestCamelToSnake_ConvertsIdentifiers      Acronym followed by a word ("APIKey")  "api_key"
// 5   TestCamelToSnake_ConvertsIdentifiers      Long PascalCase                        "create_user_request"
// 6   TestCamelToSnake_ConvertsIdentifiers      Empty string                           Empty string
// 7   TestNormalizeFieldPath_ConvertsNamespace  Root struct name present               Root dropped
// 8   TestNormalizeFieldPath_ConvertsNamespace  CamelCase segment                      Converted to snake_case
// 9   TestNormalizeFieldPath_ConvertsNamespace  Embedded Request segment               Segment dropped
// 10  TestNormalizeFieldPath_ConvertsNamespace  Nested struct path                     Dot separators kept
// 11  TestNormalizeFieldPath_ConvertsNamespace  Slice index in the path                Index kept on its segment
//
// =============================================================================

import "testing"

func TestCamelToSnake_ConvertsIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simple two words", in: "UserName", want: "user_name"},
		{name: "already lowercase", in: "email", want: "email"},
		{name: "consecutive capitals treated as an acronym", in: "UserID", want: "user_id"},
		{name: "acronym followed by a word", in: "APIKey", want: "api_key"},
		{name: "long PascalCase", in: "CreateUserRequest", want: "create_user_request"},
		{name: "empty string", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := camelToSnake(tt.in); got != tt.want {
				t.Fatalf("camelToSnake(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeFieldPath_ConvertsNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		want      string
	}{
		{
			name:      "drops the root struct name",
			namespace: "CreateUserRequest.Password",
			want:      "password",
		},
		{
			name:      "converts each segment to snake_case",
			namespace: "CreateUserRequest.PhoneNumber",
			want:      "phone_number",
		},
		{
			name:      "drops an embedded Request segment",
			namespace: "ListUsersRequest.Request.Page",
			want:      "page",
		},
		{
			name:      "nested struct path keeps dot separators",
			namespace: "CreateOrderRequest.Address.City",
			want:      "address.city",
		},
		{
			name:      "slice index is preserved on its own segment",
			namespace: "CreateOrderRequest.Items[0].Name",
			want:      "items[0].name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeFieldPath(tt.namespace); got != tt.want {
				t.Fatalf("normalizeFieldPath(%q) = %q, want %q", tt.namespace, got, tt.want)
			}
		})
	}
}
