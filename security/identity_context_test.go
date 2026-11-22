package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: identity_context_test.go
// Purpose: Verify storing and reading the caller identity in a context.
// Type: Unit
//
// Test functions: 3
// Total scenarios: 5
//
// #  Test Case                                      Scenario                           Expected Result
// 1  TestGetIdentity_ReturnsStoredIdentity          Identity stored with WithIdentity  Returns the same identity and ok
// 2  TestGetIdentity_ReportsMissingIdentity         No identity in the context         Returns not ok
// 3  TestGetIdentity_ReportsMissingIdentity         Identity of another type           Returns not ok
// 4  TestGetIdentity_ReportsMissingIdentity         Value stored under a string key    Returns not ok
// 5  TestMustGetIdentity_PanicsWhenIdentityMissing  No identity in the context         Panics
//
// =============================================================================

import (
	"context"
	"testing"

	"github.com/jun-develop/junkit/security"
)

type testIdentity struct{ UserID int64 }

func TestGetIdentity_ReturnsStoredIdentity(t *testing.T) {
	want := &testIdentity{UserID: 42}
	ctx := security.WithIdentity(context.Background(), want)

	got, ok := security.GetIdentity[*testIdentity](ctx)

	if !ok {
		t.Fatal("GetIdentity() ok = false, want true")
	}

	if got != want {
		t.Errorf("GetIdentity() = %v, want %v", got, want)
	}
}

func TestGetIdentity_ReportsMissingIdentity(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"no identity in the context", context.Background()},
		{"identity of another type", security.WithIdentity(context.Background(), "a string identity")},
		{"value stored under a string key", context.WithValue(context.Background(), "identityKey", &testIdentity{UserID: 1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := security.GetIdentity[*testIdentity](tt.ctx); ok {
				t.Error("GetIdentity() ok = true, want false")
			}
		})
	}
}

func TestMustGetIdentity_PanicsWhenIdentityMissing(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustGetIdentity() did not panic, want a panic for a missing identity")
		}
	}()

	security.MustGetIdentity[*testIdentity](context.Background())
}
