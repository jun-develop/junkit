package security_test

import (
	"context"
	"testing"

	"github.com/jun-develop/junkit/security"
)

type testIdentity struct{ UserID int64 }

func TestIdentity_WithAndGet(t *testing.T) {
	want := &testIdentity{UserID: 42}
	ctx := security.WithIdentity(context.Background(), want)

	if got, ok := security.GetIdentity[*testIdentity](ctx); !ok || got != want {
		t.Errorf("GetIdentity() = %v, %v, want %v", got, ok, want)
	}
}

func TestIdentity_WrongTypeOrMissing(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"missing", context.Background()},
		{"wrong type", security.WithIdentity(context.Background(), "a string identity")},
		{"string key", context.WithValue(context.Background(), "identityKey", &testIdentity{UserID: 1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := security.GetIdentity[*testIdentity](tt.ctx); ok {
				t.Error("GetIdentity() reported ok")
			}
		})
	}
}

func TestMustGetIdentity_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustGetIdentity did not panic on missing identity")
		}
	}()

	security.MustGetIdentity[*testIdentity](context.Background())
}
