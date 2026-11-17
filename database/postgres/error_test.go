package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jun-develop/junkit/database"
)

// pgError returns a PgError with code and a fixed message and constraint name.
func pgError(code string) *pgconn.PgError {
	return &pgconn.PgError{Code: code, Message: "boom", ConstraintName: "uq_test"}
}

func TestClassify_MapsPgErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantType   database.ErrorType
		wantDetail string
	}{
		{
			name:       "nil error",
			err:        nil,
			wantType:   database.ErrorNone,
			wantDetail: "",
		},
		{
			name:       "pgx.ErrNoRows",
			err:        pgx.ErrNoRows,
			wantType:   database.ErrorNotFound,
			wantDetail: "resource not found",
		},
		{
			name:       "wrapped pgx.ErrNoRows is still recognized",
			err:        fmt.Errorf("query row: %w", pgx.ErrNoRows),
			wantType:   database.ErrorNotFound,
			wantDetail: "resource not found",
		},
		{
			name:     "unique_violation",
			err:      pgError("23505"),
			wantType: database.ErrorUniqueViolation,
		},
		{
			name:     "foreign_key_violation",
			err:      pgError("23503"),
			wantType: database.ErrorForeignKey,
		},
		{
			name:     "check_violation",
			err:      pgError("23514"),
			wantType: database.ErrorCheckViolation,
		},
		{
			name:       "insufficient_privilege",
			err:        pgError("42501"),
			wantType:   database.ErrorPermissionDenied,
			wantDetail: "permission denied",
		},
		{
			name:       "connection_failure",
			err:        pgError("08006"),
			wantType:   database.ErrorConnection,
			wantDetail: "database connection error",
		},
		{
			name:       "invalid_text_representation",
			err:        pgError("22P02"),
			wantType:   database.ErrorInvalidInput,
			wantDetail: "invalid input format",
		},
		{
			name:     "raise_exception carries the raw message through",
			err:      &pgconn.PgError{Code: "P0001", Message: "insufficient balance"},
			wantType: database.ErrorBusiness, wantDetail: "insufficient balance",
		},
		{
			name:     "unrecognized SQLSTATE falls back to unknown",
			err:      pgError("99999"),
			wantType: database.ErrorUnknown,
		},
		{
			name:       "context deadline exceeded",
			err:        context.DeadlineExceeded,
			wantType:   database.ErrorConnection,
			wantDetail: "query timeout or canceled",
		},
		{
			name:       "context canceled",
			err:        context.Canceled,
			wantType:   database.ErrorConnection,
			wantDetail: "query timeout or canceled",
		},
		{
			name:     "an ordinary error is unknown",
			err:      errors.New("something else broke"),
			wantType: database.ErrorUnknown, wantDetail: "something else broke",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotDetail := database.Classify(tt.err)

			if gotType != tt.wantType {
				t.Fatalf("Classify() type = %q, want %q", gotType, tt.wantType)
			}

			if tt.wantDetail != "" && gotDetail != tt.wantDetail {
				t.Fatalf("Classify() detail = %q, want %q", gotDetail, tt.wantDetail)
			}
		})
	}
}

func TestClassify_IncludesConstraintNameInDetail(t *testing.T) {
	_, detail := database.Classify(pgError("23505"))

	if !strings.Contains(detail, "uq_test") {
		t.Fatalf("Classify() detail = %q, want it to mention constraint %q", detail, "uq_test")
	}
}

func TestPredicates_MatchPgErrors(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantNotFound   bool
		wantUnique     bool
		wantForeignKey bool
		wantCheck      bool
		wantConnection bool
	}{
		{name: "nil error matches nothing", err: nil},
		{name: "not found", err: pgx.ErrNoRows, wantNotFound: true},
		{name: "unique violation", err: pgError("23505"), wantUnique: true},
		{name: "foreign key violation", err: pgError("23503"), wantForeignKey: true},
		{name: "check violation", err: pgError("23514"), wantCheck: true},
		{name: "connection error", err: pgError("08006"), wantConnection: true},
		{name: "unrelated error matches nothing", err: errors.New("boom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := database.IsNotFound(tt.err); got != tt.wantNotFound {
				t.Errorf("IsNotFound() = %v, want %v", got, tt.wantNotFound)
			}

			if got := database.IsUniqueViolation(tt.err); got != tt.wantUnique {
				t.Errorf("IsUniqueViolation() = %v, want %v", got, tt.wantUnique)
			}

			if got := database.IsForeignKeyViolation(tt.err); got != tt.wantForeignKey {
				t.Errorf("IsForeignKeyViolation() = %v, want %v", got, tt.wantForeignKey)
			}

			if got := database.IsCheckViolation(tt.err); got != tt.wantCheck {
				t.Errorf("IsCheckViolation() = %v, want %v", got, tt.wantCheck)
			}

			if got := database.IsConnectionError(tt.err); got != tt.wantConnection {
				t.Errorf("IsConnectionError() = %v, want %v", got, tt.wantConnection)
			}
		})
	}
}

func TestMatch_TranslatesPgErrors(t *testing.T) {
	errEmailExists := errors.New("email already exists")
	errNotFound := errors.New("user not found")

	mapping := map[database.ErrorType]error{
		database.ErrorUniqueViolation: errEmailExists,
		database.ErrorNotFound:        errNotFound,
	}

	t.Run("nil error returns nil", func(t *testing.T) {
		if got := database.Match(nil, mapping); got != nil {
			t.Fatalf("Match(nil, ...) = %v, want nil", got)
		}
	})

	t.Run("mapped error type is translated", func(t *testing.T) {
		got := database.Match(pgError("23505"), mapping)
		if !errors.Is(got, errEmailExists) {
			t.Fatalf("Match() = %v, want %v", got, errEmailExists)
		}
	})

	t.Run("unmapped error type passes through unchanged", func(t *testing.T) {
		// A foreign key violation has no entry in mapping.
		original := pgError("23503")
		got := database.Match(original, mapping)
		if !errors.Is(got, original) {
			t.Fatalf("Match() = %v, want the original error unchanged", got)
		}
	})
}
