package database_test

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

			if gotType != tt.wantType || gotDetail != tt.wantDetail {
				t.Fatalf("Classify() = %q, %q, want %q, %q", gotType, gotDetail, tt.wantType, tt.wantDetail)
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

	if got := database.MatchNotFound(nil, errUserNotFound); got != nil {
		t.Errorf("MatchNotFound(nil) = %v, want nil", got)
	}

	if got := database.MatchNotFound(fmt.Errorf("get user: %w", errNoRows), errUserNotFound); !errors.Is(got, errUserNotFound) {
		t.Errorf("MatchNotFound(not found) = %v, want %v", got, errUserNotFound)
	}

	if got := database.MatchNotFound(errDuplicate, errUserNotFound); !errors.Is(got, errDuplicate) {
		t.Errorf("MatchNotFound(unique violation) = %v, want the error unchanged", got)
	}
}

func TestWrapper_PrefixesAndWraps(t *testing.T) {
	wrap := database.Wrapper("user repository")

	if got := wrap("get user", nil); got != nil {
		t.Errorf("wrap(nil) = %v, want nil", got)
	}

	got := wrap("get user", errNoRows)
	if want := "user repository: get user: fake driver: no rows"; got.Error() != want {
		t.Errorf("wrap() = %q, want %q", got, want)
	}

	if !errors.Is(got, errNoRows) {
		t.Errorf("wrap() = %v, want it to wrap the original error", got)
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
