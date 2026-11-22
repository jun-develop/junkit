package database_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: error_test.go
// Purpose: Verify engine-neutral error classification, translation (Match, MatchNotFound),
//          error prefixing and the Is* predicates, using a fake driver classifier.
// Type: Unit
//
// Test functions: 5
// Total scenarios: 20
//
// #   Test Case                                 Scenario                                    Expected Result
// 1   TestClassify_MapsErrors                   nil error                                   ErrorNone, empty detail
// 2   TestClassify_MapsErrors                   Error the registered classifier recognizes  The classifier's type and detail
// 3   TestClassify_MapsErrors                   Wrapped recognized error                    The classifier's type and detail
// 4   TestClassify_MapsErrors                   context.DeadlineExceeded                    ErrorConnection, "query timeout or canceled"
// 5   TestClassify_MapsErrors                   context.Canceled                            ErrorConnection, "query timeout or canceled"
// 6   TestClassify_MapsErrors                   Unrecognized error                          ErrorUnknown, the error message as detail
// 7   TestMatch_TranslatesMappedTypes           Unique violation with a mapping for it      Returns the mapped error; IsUniqueViolation is true
// 8   TestMatchNotFound_TranslatesOnlyNotFound  nil error                                   Returns nil
// 9   TestMatchNotFound_TranslatesOnlyNotFound  Wrapped not-found error                     Returns the given not-found error
// 10  TestMatchNotFound_TranslatesOnlyNotFound  Unique violation                            Returns the error unchanged
// 11  TestErrorPrefixer_PrefixesAndWraps        nil and a driver error                      nil stays nil; error is prefixed and still wraps the original
// 12  TestPredicates_MatchTheirErrorType        nil error                                   No predicate matches
// 13  TestPredicates_MatchTheirErrorType        Not found                                   Only IsNotFound
// 14  TestPredicates_MatchTheirErrorType        Wrapped not found                           Only IsNotFound
// 15  TestPredicates_MatchTheirErrorType        Unique violation                            Only IsUniqueViolation
// 16  TestPredicates_MatchTheirErrorType        Foreign key violation                       Only IsForeignKeyViolation
// 17  TestPredicates_MatchTheirErrorType        Check violation                             Only IsCheckViolation
// 18  TestPredicates_MatchTheirErrorType        context.DeadlineExceeded                    Only IsConnectionError
// 19  TestPredicates_MatchTheirErrorType        context.Canceled                            Only IsConnectionError
// 20  TestPredicates_MatchTheirErrorType        Unrelated error                             No predicate matches
//
// =============================================================================

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jun-develop/junkit/database"
)

// Fake driver errors that the classifier registered in init maps to each ErrorType.
var (
	errDuplicate  = errors.New("fake driver: duplicate key")
	errNoRows     = errors.New("fake driver: no rows")
	errForeignKey = errors.New("fake driver: foreign key")
	errCheck      = errors.New("fake driver: check")
)

// init registers a fake driver Classifier, as an engine package would.
func init() {
	database.RegisterClassifier(func(err error) (database.ErrorType, string, bool) {
		switch {
		case errors.Is(err, errDuplicate):
			return database.ErrorUniqueViolation, "duplicate", true
		case errors.Is(err, errNoRows):
			return database.ErrorNotFound, "no rows", true
		case errors.Is(err, errForeignKey):
			return database.ErrorForeignKey, "foreign key", true
		case errors.Is(err, errCheck):
			return database.ErrorCheckViolation, "check", true
		}

		return database.ErrorNone, "", false
	})
}

func TestClassify_MapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantType   database.ErrorType
		wantDetail string
	}{
		{name: "nil error", err: nil, wantType: database.ErrorNone, wantDetail: ""},
		{name: "a registered classifier recognizes its error", err: errDuplicate, wantType: database.ErrorUniqueViolation, wantDetail: "duplicate"},
		{name: "wrapped errors reach the classifier", err: fmt.Errorf("insert: %w", errDuplicate), wantType: database.ErrorUniqueViolation, wantDetail: "duplicate"},
		{name: "context deadline exceeded", err: context.DeadlineExceeded, wantType: database.ErrorConnection, wantDetail: "query timeout or canceled"},
		{name: "context canceled", err: context.Canceled, wantType: database.ErrorConnection, wantDetail: "query timeout or canceled"},
		{name: "an error no classifier recognizes is unknown", err: errors.New("something else broke"), wantType: database.ErrorUnknown, wantDetail: "something else broke"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotDetail := database.Classify(tt.err)

			if gotType != tt.wantType {
				t.Errorf("Classify() type = %q, want %q", gotType, tt.wantType)
			}

			if gotDetail != tt.wantDetail {
				t.Errorf("Classify() detail = %q, want %q", gotDetail, tt.wantDetail)
			}
		})
	}
}

func TestMatch_TranslatesMappedTypes(t *testing.T) {
	errEmailExists := errors.New("email already exists")
	mapping := map[database.ErrorType]error{database.ErrorUniqueViolation: errEmailExists}

	if got := database.Match(errDuplicate, mapping); !errors.Is(got, errEmailExists) {
		t.Fatalf("Match() = %v, want %v", got, errEmailExists)
	}

	if !database.IsUniqueViolation(errDuplicate) {
		t.Fatal("IsUniqueViolation() = false for an error the registered classifier maps to a unique violation")
	}
}

func TestMatchNotFound_TranslatesOnlyNotFound(t *testing.T) {
	errUserNotFound := errors.New("user not found")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "nil error", err: nil, want: nil},
		{name: "wrapped not-found error", err: fmt.Errorf("get user: %w", errNoRows), want: errUserNotFound},
		{name: "unique violation is returned unchanged", err: errDuplicate, want: errDuplicate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// errors.Is(got, nil) is true only when got is nil.
			if got := database.MatchNotFound(tt.err, errUserNotFound); !errors.Is(got, tt.want) {
				t.Errorf("MatchNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestErrorPrefixer_PrefixesAndWraps(t *testing.T) {
	prefixErr := database.ErrorPrefixer("user repository")

	if got := prefixErr("get user", nil); got != nil {
		t.Errorf("prefixErr(nil) = %v, want nil", got)
	}

	got := prefixErr("get user", errNoRows)
	if want := "user repository: get user: fake driver: no rows"; got.Error() != want {
		t.Errorf("prefixErr() = %q, want %q", got, want)
	}

	if !errors.Is(got, errNoRows) {
		t.Errorf("prefixErr() = %v, want it to wrap the original error", got)
	}
}

func TestPredicates_MatchTheirErrorType(t *testing.T) {
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
		{name: "not found", err: errNoRows, wantNotFound: true},
		{name: "wrapped not found", err: fmt.Errorf("get: %w", errNoRows), wantNotFound: true},
		{name: "unique violation", err: errDuplicate, wantUnique: true},
		{name: "foreign key violation", err: errForeignKey, wantForeignKey: true},
		{name: "check violation", err: errCheck, wantCheck: true},
		{name: "deadline exceeded is a connection error", err: context.DeadlineExceeded, wantConnection: true},
		{name: "canceled is a connection error", err: context.Canceled, wantConnection: true},
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
