package validation

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: helper_internal_test.go
// Purpose: Verify normalizeFieldPath turns validator namespaces into snake_case error keys.
// Type: Unit
//
// Test functions: 1
// Total scenarios: 5
//
// #  Test Case                                 Scenario                  Expected Result
// 1  TestNormalizeFieldPath_ConvertsNamespace  Root struct name present  Root dropped
// 2  TestNormalizeFieldPath_ConvertsNamespace  CamelCase segment         Converted to snake_case
// 3  TestNormalizeFieldPath_ConvertsNamespace  Embedded Request segment  Segment dropped
// 4  TestNormalizeFieldPath_ConvertsNamespace  Nested struct path        Dot separators kept
// 5  TestNormalizeFieldPath_ConvertsNamespace  Slice index in the path   Index kept on its segment
//
// =============================================================================

import "testing"

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
