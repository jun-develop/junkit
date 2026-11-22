package validation

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: custom_validation_internal_test.go
// Purpose: Verify the email domain checker (DNS lookups, cache, blocklist) with a fake resolver.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 19
//
// #   Test Case                                         Scenario                                         Expected Result
// 1   TestEmailChecker_ValidatesDomain                  Domain has an MX record                          Valid, 1 lookup
// 2   TestEmailChecker_ValidatesDomain                  No MX but an A record (implicit MX)              Valid, 2 lookups
// 3   TestEmailChecker_ValidatesDomain                  Domain does not exist                            Invalid, 2 lookups
// 4   TestEmailChecker_ValidatesDomain                  MX lookup fails transiently                      Valid (fail open), 1 lookup
// 5   TestEmailChecker_ValidatesDomain                  Host lookup fails transiently                    Valid (fail open), 2 lookups
// 6   TestEmailChecker_ValidatesDomain                  Malformed address                                Invalid, no lookup
// 7   TestEmailChecker_ValidatesDomain                  Disposable domain in upper case                  Invalid, no lookup
// 8   TestEmailChecker_ValidatesDomain                  Dotless domain                                   Invalid, no lookup
// 9   TestEmailChecker_ValidatesDomain                  IP literal domain                                Invalid, no lookup
// 10  TestEmailChecker_CachesLookups                    Positive result                                  Second check reuses the cache
// 11  TestEmailChecker_CachesLookups                    Negative result                                  Second check reuses the cache
// 12  TestEmailChecker_CachesLookups                    Transient failure                                Second check looks up again
// 13  TestEmailChecker_CachesLookups                    Expired cache entry                              Looked up again, fresh result used
// 14  TestLoadBlocklist_ReadsDomains                    Empty path                                       Seed list is used
// 15  TestLoadBlocklist_ReadsDomains                    Path to a file                                   File replaces the seed list; lowercased, trimmed, blanks skipped
// 16  TestLoadBlocklist_ReadsDomains                    Missing file                                     Returns an error
// 17  TestEmailChecker_RejectsDomainsFromBlocklistFile  File entry ANOTHER.example, email in lower case  Rejected before any DNS lookup
// 18  TestEmailChecker_RejectsDomainsFromBlocklistFile  Same entry, email in mixed case                  Rejected before any DNS lookup
// 19  TestEmailChecker_RejectsDomainsFromBlocklistFile  Seed-list domain not in the file                 Accepted (file replaces the seed list)
//
// =============================================================================

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// notFound and temporary are the DNS answers the email check tells apart.
var (
	notFound  = &net.DNSError{Err: "no such host", IsNotFound: true}
	temporary = &net.DNSError{Err: "server misbehaving", IsTemporary: true}
)

// fakeResolver answers every lookup with fixed results and counts the calls.
type fakeResolver struct {
	mxs     []*net.MX
	mxErr   error
	hosts   []string
	hostErr error
	calls   int
}

// LookupMX implements resolver.
func (r *fakeResolver) LookupMX(context.Context, string) ([]*net.MX, error) {
	r.calls++

	return r.mxs, r.mxErr
}

// LookupHost implements resolver.
func (r *fakeResolver) LookupHost(context.Context, string) ([]string, error) {
	r.calls++

	return r.hosts, r.hostErr
}

// newTestChecker returns an emailChecker with the seed blocklist that resolves through r.
func newTestChecker(t *testing.T, r resolver) *emailChecker {
	t.Helper()
	c, err := newEmailChecker(Options{}.withDefaults())
	if err != nil {
		t.Fatalf("newEmailChecker() error = %v", err)
	}

	c.resolver = r

	return c
}

func TestEmailChecker_ValidatesDomain(t *testing.T) {
	mx := []*net.MX{{Host: "mx.example.com.", Pref: 10}}

	tests := []struct {
		name      string
		email     string
		resolver  *fakeResolver
		want      bool
		wantCalls int
	}{
		{"domain has an mx record", "a@example.com", &fakeResolver{mxs: mx}, true, 1},
		{"no mx but an a record (implicit mx)", "a@example.com",
			&fakeResolver{mxErr: notFound, hosts: []string{"192.0.2.1"}}, true, 2},
		{"domain does not exist", "a@example.com", &fakeResolver{mxErr: notFound, hostErr: notFound}, false, 2},
		{"mx lookup fails transiently", "a@example.com", &fakeResolver{mxErr: temporary}, true, 1},
		{"host lookup fails transiently", "a@example.com",
			&fakeResolver{mxErr: notFound, hostErr: temporary}, true, 2},
		{"malformed address", "not-an-email", &fakeResolver{}, false, 0},
		{"disposable domain in upper case", "a@MAILINATOR.com", &fakeResolver{mxs: mx}, false, 0},
		{"dotless domain", "a@localhost", &fakeResolver{mxs: mx}, false, 0},
		{"ip literal domain", "a@192.0.2.1", &fakeResolver{mxs: mx}, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestChecker(t, tt.resolver)

			if got := c.valid(tt.email); got != tt.want {
				t.Fatalf("valid(%q) = %v, want %v", tt.email, got, tt.want)
			}

			if tt.resolver.calls != tt.wantCalls {
				t.Fatalf("resolver calls = %d, want %d", tt.resolver.calls, tt.wantCalls)
			}
		})
	}
}

func TestEmailChecker_CachesLookups(t *testing.T) {
	tests := []struct {
		name      string
		resolver  *fakeResolver
		wantCalls int
	}{
		{"positive result is cached", &fakeResolver{mxs: []*net.MX{{Host: "mx.example.com."}}}, 1},
		{"negative result is cached", &fakeResolver{mxErr: notFound, hostErr: notFound}, 2},
		{"transient failure is retried", &fakeResolver{mxErr: temporary}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestChecker(t, tt.resolver)

			first := c.valid("a@example.com")
			second := c.valid("b@EXAMPLE.com")

			if second != first {
				t.Fatalf("second check = %v, want %v (same as the first)", second, first)
			}

			if tt.resolver.calls != tt.wantCalls {
				t.Fatalf("resolver calls = %d, want %d", tt.resolver.calls, tt.wantCalls)
			}
		})
	}

	t.Run("expired entry is looked up again", func(t *testing.T) {
		r := &fakeResolver{mxs: []*net.MX{{Host: "mx.example.com."}}}
		c := newTestChecker(t, r)
		c.cache["example.com"] = mxResult{ok: false, expires: time.Now().Add(-time.Second)}

		got := c.valid("a@example.com")

		if !got {
			t.Fatal("valid() = false, want true from a fresh lookup (the expired entry was reused)")
		}

		if r.calls != 1 {
			t.Fatalf("resolver calls = %d, want 1", r.calls)
		}
	})
}

func TestLoadBlocklist_ReadsDomains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocklist.conf")
	if err := os.WriteFile(path, []byte("throwaway.example\n\n  ANOTHER.example  \n"), 0o644); err != nil {
		t.Fatalf("write blocklist: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		want    []string
		notWant []string
		wantErr bool
	}{
		{name: "empty path uses the seed list", want: []string{"mailinator.com"}},
		{name: "file replaces the seed list", path: path,
			want: []string{"throwaway.example", "another.example"}, notWant: []string{"mailinator.com", ""}},
		{name: "missing file", path: filepath.Join(dir, "missing.conf"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadBlocklist(tt.path)

			if (err != nil) != tt.wantErr {
				t.Fatalf("loadBlocklist() error = %v, wantErr %v", err, tt.wantErr)
			}

			for _, d := range tt.want {
				if !got[d] {
					t.Errorf("blocklist missing %q", d)
				}
			}

			for _, d := range tt.notWant {
				if got[d] {
					t.Errorf("blocklist unexpectedly has %q", d)
				}
			}
		})
	}
}

func TestEmailChecker_RejectsDomainsFromBlocklistFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocklist.conf")
	if err := os.WriteFile(path, []byte("ANOTHER.example\n"), 0o644); err != nil {
		t.Fatalf("write blocklist: %v", err)
	}

	// The resolver accepts every domain, so only the blocklist can reject an address.
	mx := []*net.MX{{Host: "mx.example.com.", Pref: 10}}

	tests := []struct {
		name      string
		email     string
		want      bool
		wantCalls int
	}{
		{"listed domain in lower case", "a@another.example", false, 0},
		{"listed domain in mixed case", "a@Another.Example", false, 0},
		{"domain not in the file", "a@mailinator.com", true, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeResolver{mxs: mx}
			c, err := newEmailChecker(Options{BlocklistPath: path}.withDefaults())
			if err != nil {
				t.Fatalf("newEmailChecker() error = %v", err)
			}

			c.resolver = r

			if got := c.valid(tt.email); got != tt.want {
				t.Errorf("valid(%q) = %v, want %v", tt.email, got, tt.want)
			}

			if r.calls != tt.wantCalls {
				t.Errorf("resolver calls = %d, want %d", r.calls, tt.wantCalls)
			}
		})
	}
}
