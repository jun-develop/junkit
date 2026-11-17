package tracer

import (
	"net/http"
	"reflect"
	"testing"
)

// TestCloneHeaders checks cloneHeaders masks secrets and returns an independent copy.
func TestCloneHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer abcdefghijklmnopqrstuvwxyz")
	h.Set("Cookie", "session=secret")
	h.Set("Content-Type", "application/json")

	clone := cloneHeaders(h)

	if clone.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want it unchanged", clone.Get("Content-Type"))
	}

	if clone.Get("Authorization") == h.Get("Authorization") {
		t.Error("Authorization should be masked in the clone")
	}

	if clone.Get("Cookie") == h.Get("Cookie") {
		t.Error("Cookie should be masked in the clone")
	}

	// Mutating the clone must not affect the original — it's a deep copy.
	clone.Set("Content-Type", "text/plain")
	if h.Get("Content-Type") != "application/json" {
		t.Error("mutating the clone leaked back into the original headers")
	}

	if got := cloneHeaders(nil); got != nil {
		t.Fatalf("cloneHeaders(nil) = %v, want nil", got)
	}
}

// TestCloneHeaders_DoesNotAliasSliceBackingArrays checks value slices are copied.
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

// TestMaskQuery_HidesSecretParams checks secret-named parameters are masked and others kept.
func TestMaskQuery_HidesSecretParams(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain params unchanged", in: "page=2&sort=name", want: "page=2&sort=name"},
		{name: "secret params masked in place", in: "q=go&access_token=abc&page=1", want: "q=go&access_token=****&page=1"},
		{name: "encoded name is decoded before matching", in: "api%5Fkey=abc", want: "api%5Fkey=****"},
		{name: "case-insensitive", in: "Password=hunter2", want: "Password=****"},
		{name: "flag without value kept", in: "debug&token=x", want: "debug&token=****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskQuery(tt.in); got != tt.want {
				t.Fatalf("maskQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCloneHeaders_MasksSecretHeaders checks every value of every secret-named header is masked.
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
