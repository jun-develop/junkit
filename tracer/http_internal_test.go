package tracer

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: http_internal_test.go
// Purpose: Verify header cloning and query-string masking used when tracing HTTP requests.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 10
//
// #   Test Case                                        Scenario                                               Expected Result
// 1   TestCloneHeaders_MasksSecretsAndCopies           Authorization, Cookie and Content-Type headers         Secrets masked, Content-Type kept, clone independent of the original
// 2   TestCloneHeaders_MasksSecretsAndCopies           Nil headers                                            Returns nil
// 3   TestCloneHeaders_DoesNotAliasSliceBackingArrays  Clone value slice mutated                              Original header values unchanged
// 4   TestCloneHeaders_MasksSecretHeaders              API key, token, proxy auth, multi-value Authorization  Every value masked; scheme kept; Accept unchanged
// 5   TestMaskQuery_HidesSecretParams                  Empty query                                            Returns an empty string
// 6   TestMaskQuery_HidesSecretParams                  Only non-secret params                                 Returned unchanged
// 7   TestMaskQuery_HidesSecretParams                  Secret param among others                              Only the secret value is masked, order kept
// 8   TestMaskQuery_HidesSecretParams                  URL-encoded secret name (api%5Fkey)                    Name decoded before matching; value masked
// 9   TestMaskQuery_HidesSecretParams                  Upper-case secret name                                 Masked (case-insensitive)
// 10  TestMaskQuery_HidesSecretParams                  Flag without a value next to a secret                  Flag kept, secret masked
//
// =============================================================================

import (
	"net/http"
	"reflect"
	"testing"
)

func TestCloneHeaders_MasksSecretsAndCopies(t *testing.T) {
	t.Run("authorization, cookie and content-type headers", func(t *testing.T) {
		h := http.Header{}
		h.Set("Authorization", "Bearer abcdefghijklmnopqrstuvwxyz")
		h.Set("Cookie", "session=secret")
		h.Set("Content-Type", "application/json")

		clone := cloneHeaders(h)

		if clone.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want it unchanged", clone.Get("Content-Type"))
		}

		if clone.Get("Authorization") == h.Get("Authorization") {
			t.Errorf("Authorization = %q, want it masked in the clone", clone.Get("Authorization"))
		}

		if clone.Get("Cookie") == h.Get("Cookie") {
			t.Errorf("Cookie = %q, want it masked in the clone", clone.Get("Cookie"))
		}

		clone.Set("Content-Type", "text/plain")
		if h.Get("Content-Type") != "application/json" {
			t.Error("mutating the clone changed the original headers, want a deep copy")
		}
	})

	t.Run("nil headers", func(t *testing.T) {
		if got := cloneHeaders(nil); got != nil {
			t.Errorf("cloneHeaders(nil) = %v, want nil", got)
		}
	})
}

func TestCloneHeaders_DoesNotAliasSliceBackingArrays(t *testing.T) {
	h := http.Header{"X-Multi": []string{"a", "b"}}
	clone := cloneHeaders(h)

	clone["X-Multi"][0] = "mutated"
	if h["X-Multi"][0] != "a" {
		t.Fatal("cloneHeaders shares backing arrays with the original — a mutation leaked through")
	}

	if !reflect.DeepEqual(h["X-Multi"], []string{"a", "b"}) {
		t.Fatalf("original header slice = %v, want unchanged", h["X-Multi"])
	}
}

func TestCloneHeaders_MasksSecretHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-Api-Key", "k-123")
	h.Set("X-Auth-Token", "t-456")
	h.Set("Proxy-Authorization", "Basic abc")
	h["Authorization"] = []string{"Bearer one", "Bearer two"}
	h.Set("Accept", "application/json")

	clone := cloneHeaders(h)

	for _, name := range []string{"X-Api-Key", "X-Auth-Token"} {
		if got := clone.Get(name); got != "****" {
			t.Errorf("%s = %q, want ****", name, got)
		}
	}

	if got := clone.Get("Proxy-Authorization"); got != "Basic ****" {
		t.Errorf("Proxy-Authorization = %q, want %q", got, "Basic ****")
	}

	if got := clone["Authorization"]; !reflect.DeepEqual(got, []string{"Bearer ****", "Bearer ****"}) {
		t.Errorf("Authorization = %v, want every value masked", got)
	}

	if got := clone.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want it unchanged", got)
	}
}

func TestMaskQuery_HidesSecretParams(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "plain params unchanged", input: "page=2&sort=name", want: "page=2&sort=name"},
		{name: "secret params masked in place", input: "q=go&access_token=abc&page=1", want: "q=go&access_token=****&page=1"},
		{name: "encoded name is decoded before matching", input: "api%5Fkey=abc", want: "api%5Fkey=****"},
		{name: "case-insensitive", input: "Password=hunter2", want: "Password=****"},
		{name: "flag without value kept", input: "debug&token=x", want: "debug&token=****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskQuery(tt.input); got != tt.want {
				t.Errorf("maskQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
