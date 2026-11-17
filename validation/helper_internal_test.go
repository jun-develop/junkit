package validation

import "testing"

func TestCamelToSnake(t *testing.T) {
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

func TestNormalizeFieldPath(t *testing.T) {
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
