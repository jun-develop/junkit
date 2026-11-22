package response_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: error_test.go
// Purpose: Verify AppError construction, wrapping and the predefined sentinel errors.
// Type: Unit
//
// Test functions: 4
// Total scenarios: 4
//
// #  Test Case                                   Scenario                                  Expected Result
// 1  TestNew_CreatesErrorWithoutCause            Code and message only                     Fields set, no cause, Error() is the message
// 2  TestWrap_IncludesAndExposesCause            Message with a cause                      Error() joins message and cause; errors.Is finds the cause
// 3  TestAppError_ErrorsAsThroughAWrappingLayer  AppError wrapped with %w                  errors.As finds it with its code
// 4  TestSentinelErrors_UseInternalCode          ErrInternalServer, ErrDatabaseConnection  Both have the Internal code
//
// =============================================================================

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jun-develop/junkit/response"
)

func TestNew_CreatesErrorWithoutCause(t *testing.T) {
	err := response.New(response.NotFound, "user_not_found")

	if err.Code != response.NotFound {
		t.Fatalf("Code = %q, want %q", err.Code, response.NotFound)
	}

	if err.Message != "user_not_found" {
		t.Fatalf("Message = %q, want %q", err.Message, "user_not_found")
	}

	if err.Cause != nil {
		t.Fatalf("Cause = %v, want nil", err.Cause)
	}

	if err.Error() != "user_not_found" {
		t.Fatalf("Error() = %q, want %q (the message only)", err.Error(), "user_not_found")
	}
}

func TestWrap_IncludesAndExposesCause(t *testing.T) {
	cause := errors.New("connection refused")
	err := response.Wrap(response.Internal, "failed_to_save_user", cause)

	if err.Error() != "failed_to_save_user: connection refused" {
		t.Fatalf("Error() = %q, want %q", err.Error(), "failed_to_save_user: connection refused")
	}

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is(err, cause) = false, want true (Unwrap must expose the cause)")
	}
}

func TestAppError_ErrorsAsThroughAWrappingLayer(t *testing.T) {
	original := response.New(response.Conflict, "email_already_exists")
	wrapped := fmt.Errorf("create user: %w", original)

	var target *response.AppError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As(wrapped, *AppError) = false, want true through a %w-wrapping layer")
	}

	if target.Code != response.Conflict {
		t.Fatalf("unwrapped Code = %q, want %q", target.Code, response.Conflict)
	}
}

func TestSentinelErrors_UseInternalCode(t *testing.T) {
	if response.ErrInternalServer.Code != response.Internal {
		t.Fatalf("ErrInternalServer.Code = %q, want %q", response.ErrInternalServer.Code, response.Internal)
	}

	if response.ErrDatabaseConnection.Code != response.Internal {
		t.Fatalf("ErrDatabaseConnection.Code = %q, want %q", response.ErrDatabaseConnection.Code, response.Internal)
	}
}
