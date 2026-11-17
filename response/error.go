package response

import "fmt"

// ErrorCode is a machine-readable identifier for an API error.
type ErrorCode string

// Standard error codes mapped to HTTP status codes.
const (
	BadRequest           ErrorCode = "BAD_REQUEST"
	Unauthorized         ErrorCode = "UNAUTHORIZED"
	NotFound             ErrorCode = "NOT_FOUND"
	Forbidden            ErrorCode = "FORBIDDEN"
	MethodNotAllowed     ErrorCode = "METHOD_NOT_ALLOWED"
	Conflict             ErrorCode = "CONFLICT"
	UnprocessableEntity  ErrorCode = "UNPROCESSABLE_ENTITY"
	UnsupportedMediaType ErrorCode = "UNSUPPORTED_MEDIA_TYPE"
	PayloadTooLarge      ErrorCode = "PAYLOAD_TOO_LARGE"
	TooManyRequests      ErrorCode = "TOO_MANY_REQUESTS"
	Internal             ErrorCode = "INTERNAL_SERVER_ERROR"
)

// Sentinel errors for cross-cutting failures common to every feature.
var (
	// ErrInternalServer is the generic mapping for untraced, unexpected failures.
	ErrInternalServer = New(Internal, "internal_server_error")

	// ErrDatabaseConnection marks a failure to reach or use the database.
	ErrDatabaseConnection = New(Internal, "database_connection_error")
)

// AppError represents a domain error with an ErrorCode, i18n Message key, and optional Cause.
type AppError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

// New creates an AppError without an underlying cause.
//
// Usage:
//
//	return response.New(response.NotFound, "user_not_found")
func New(code ErrorCode, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Wrap creates an AppError around an underlying error.
//
// Usage:
//
//	if err != nil {
//	    return response.Wrap(response.Internal, "failed_to_save_user", err)
//	}
func Wrap(code ErrorCode, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Cause: err}
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}

	return e.Message
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *AppError) Unwrap() error {
	return e.Cause
}
