package database

import (
	"context"
	"errors"
)

// ErrorType categorizes a database failure into a class a repository can
// branch on, independent of the underlying driver's error type.
type ErrorType string

// Error classes recognized by Classify.
const (
	ErrorNone             ErrorType = ""
	ErrorNotFound         ErrorType = "not_found"
	ErrorUniqueViolation  ErrorType = "unique_violation"
	ErrorForeignKey       ErrorType = "foreign_key_violation"
	ErrorCheckViolation   ErrorType = "check_violation"
	ErrorConnection       ErrorType = "connection_error"
	ErrorInvalidInput     ErrorType = "invalid_input"
	ErrorPermissionDenied ErrorType = "permission_denied"
	ErrorBusiness         ErrorType = "business_error"
	ErrorUnknown          ErrorType = "unknown"
)

// Classifier maps a driver error to an ErrorType and detail; ok is false when err is not one of
// the driver's errors.
type Classifier func(err error) (errType ErrorType, detail string, ok bool)

// classifiers is written only from engine packages' init functions, so reads need no lock.
var classifiers []Classifier

// RegisterClassifier adds an engine's Classifier to Classify. Call it only from an init function.
//
// Usage:
//
//	func init() { database.RegisterClassifier(classify) }
func RegisterClassifier(c Classifier) {
	classifiers = append(classifiers, c)
}

// Classify maps err to an ErrorType and a human-readable detail message, trying every registered
// engine Classifier in order.
//
// Usage:
//
//	if errType, detail := database.Classify(err); errType == database.ErrorUniqueViolation {
//	    return response.New(response.Conflict, "email_already_exists")
//	}
func Classify(err error) (ErrorType, string) {
	if err == nil {
		return ErrorNone, ""
	}

	for _, c := range classifiers {
		if errType, detail, ok := c(err); ok {
			return errType, detail
		}
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrorConnection, "query timeout or canceled"
	}

	return ErrorUnknown, err.Error()
}

// IsNotFound reports whether err represents a missing row.
func IsNotFound(err error) bool {
	t, _ := Classify(err)
	return t == ErrorNotFound
}

// IsUniqueViolation reports whether err represents a unique-constraint violation.
func IsUniqueViolation(err error) bool {
	t, _ := Classify(err)
	return t == ErrorUniqueViolation
}

// IsForeignKeyViolation reports whether err represents a foreign-key constraint violation.
func IsForeignKeyViolation(err error) bool {
	t, _ := Classify(err)
	return t == ErrorForeignKey
}

// IsCheckViolation reports whether err represents a check-constraint violation.
func IsCheckViolation(err error) bool {
	t, _ := Classify(err)
	return t == ErrorCheckViolation
}

// IsConnectionError reports whether err represents a connectivity or timeout failure.
func IsConnectionError(err error) bool {
	t, _ := Classify(err)
	return t == ErrorConnection
}

// Match translates a database error into a domain error using the provided mapping.
//
// Usage:
//
//	err = database.Match(err, map[database.ErrorType]error{
//	    database.ErrorUniqueViolation: domain.ErrEmailAlreadyExists,
//	    database.ErrorNotFound:        domain.ErrUserNotFound,
//	})
func Match(err error, mapping map[ErrorType]error) error {
	if err == nil {
		return nil
	}

	errType, _ := Classify(err)
	if mapped, ok := mapping[errType]; ok {
		return mapped
	}

	return err
}

// MatchNotFound returns notFound when err is a not-found error, otherwise err (nil stays nil).
//
// Usage:
//
//	return nil, database.MatchNotFound(err, domain.ErrUserNotFound)
func MatchNotFound(err, notFound error) error {
	return Match(err, map[ErrorType]error{ErrorNotFound: notFound})
}
