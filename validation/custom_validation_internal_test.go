package validation

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

func TestEmailChecker_Valid(t *testing.T) {
	mx := []*net.MX{{Host: "mx.example.com.", Pref: 10}}

	tests := []struct {
		name      string
		email     string
		resolver  *fakeResolver
		want      bool
		wantCalls int
	}{
		{"MX record", "a@example.com", &fakeResolver{mxs: mx}, true, 1},
		{"no MX but an A record (implicit MX)", "a@example.com",
			&fakeResolver{mxErr: notFound, hosts: []string{"192.0.2.1"}}, true, 2},
		{"no such host", "a@example.com", &fakeResolver{mxErr: notFound, hostErr: notFound}, false, 2},
		{"MX lookup fails transiently", "a@example.com", &fakeResolver{mxErr: temporary}, true, 1},
		{"host lookup fails transiently", "a@example.com",
			&fakeResolver{mxErr: notFound, hostErr: temporary}, true, 2},
		{"malformed", "not-an-email", &fakeResolver{}, false, 0},
		{"disposable domain", "a@MAILINATOR.com", &fakeResolver{mxs: mx}, false, 0},
		{"dotless domain", "a@localhost", &fakeResolver{mxs: mx}, false, 0},
		{"IP literal", "a@192.0.2.1", &fakeResolver{mxs: mx}, false, 0},
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

func TestEmailChecker_Cache(t *testing.T) {
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
			if second := c.valid("b@EXAMPLE.com"); second != first {
				t.Fatalf("second check = %v, first = %v", second, first)
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

		if !c.valid("a@example.com") || r.calls != 1 {
			t.Fatalf("expired entry was reused (calls = %d)", r.calls)
		}
	})
}

func TestLoadBlocklist(t *testing.T) {
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
