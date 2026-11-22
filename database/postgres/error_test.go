package postgres_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: error_test.go
// Purpose: Verify that the postgres classifier maps pgx/PgError values to database.ErrorType,
//          and that database's predicates and Match work on those errors.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 25
//
// #   Test Case                                    Scenario                                    Expected Result
// 1   TestClassify_MapsPgErrors                    nil error                                   ErrorNone
// 2   TestClassify_MapsPgErrors                    pgx.ErrNoRows                               ErrorNotFound, "resource not found"
// 3   TestClassify_MapsPgErrors                    Wrapped pgx.ErrNoRows                       ErrorNotFound, "resource not found"
// 4   TestClassify_MapsPgErrors                    SQLSTATE 23505 unique_violation             ErrorUniqueViolation
// 5   TestClassify_MapsPgErrors                    SQLSTATE 23503 foreign_key_violation        ErrorForeignKey
// 6   TestClassify_MapsPgErrors                    SQLSTATE 23514 check_violation              ErrorCheckViolation
// 7   TestClassify_MapsPgErrors                    SQLSTATE 42501 insufficient_privilege       ErrorPermissionDenied, "permission denied"
// 8   TestClassify_MapsPgErrors                    SQLSTATE 08006 connection_failure           ErrorConnection, "database connection error"
// 9   TestClassify_MapsPgErrors                    SQLSTATE 22P02 invalid_text_representation  ErrorInvalidInput, "invalid input format"
// 10  TestClassify_MapsPgErrors                    SQLSTATE P0001 raise_exception              ErrorBusiness with the raw message
// 11  TestClassify_MapsPgErrors                    Unrecognized SQLSTATE                       ErrorUnknown
// 12  TestClassify_MapsPgErrors                    context.DeadlineExceeded                    ErrorConnection, "query timeout or canceled"
// 13  TestClassify_MapsPgErrors                    context.Canceled                            ErrorConnection, "query timeout or canceled"
// 14  TestClassify_MapsPgErrors                    Ordinary error                              ErrorUnknown, the error message as detail
// 15  TestClassify_IncludesConstraintNameInDetail  Unique violation on constraint uq_test      Detail mentions uq_test
// 16  TestPredicates_MatchPgErrors                 nil error                                   No predicate matches
// 17  TestPredicates_MatchPgErrors                 pgx.ErrNoRows                               Only IsNotFound
// 18  TestPredicates_MatchPgErrors                 Unique violation                            Only IsUniqueViolation
// 19  TestPredicates_MatchPgErrors                 Foreign key violation                       Only IsForeignKeyViolation
// 20  TestPredicates_MatchPgErrors                 Check violation                             Only IsCheckViolation
// 21  TestPredicates_MatchPgErrors                 Connection failure                          Only IsConnectionError
// 22  TestPredicates_MatchPgErrors                 Unrelated error                             No predicate matches
// 23  TestMatch_TranslatesPgErrors                 nil error                                   Returns nil
// 24  TestMatch_TranslatesPgErrors                 Mapped unique violation                     Returns the mapped error
// 25  TestMatch_TranslatesPgErrors                 Unmapped foreign key violation              Returns the original error unchanged
//
// =============================================================================

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
		name     string
		err      error
		wantType database.ErrorType
		// wantDetail "" means the detail is not checked.
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
