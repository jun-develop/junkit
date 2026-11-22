package validation_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: validation_test.go
// Purpose: Verify how binding errors become field-keyed messages, and shared helpers for this package's tests.
// Type: Unit
//
// Test functions: 3
// Total scenarios: 15
//
// #   Test Case                                                  Scenario                                    Expected Result
// 1   TestValidator_GetValidationErrors_ReportsFieldMessages     nil error                                   Returns nil
// 2   TestValidator_GetValidationErrors_ReportsFieldMessages     Missing required field                      {"email": "email is required"}
// 3   TestValidator_GetValidationErrors_ReportsFieldMessages     min on a string field                       Character-count message
// 4   TestValidator_GetValidationErrors_ReportsFieldMessages     min on an int field                         Value-comparison message
// 5   TestValidator_GetValidationErrors_ReportsFieldMessages     JSON string where an int is expected        "age must be integer"
// 6   TestValidator_GetValidationErrors_ReportsFieldMessages     Non-numeric query value                     "page must be integer"
// 7   TestValidator_GetValidationErrors_ReportsFieldMessages     Negative value for an unsigned query field  "limit must be positive integer"
// 8   TestValidator_GetValidationErrors_ReportsFieldMessages     JSON type error on a *time.Time field       "start_at must be datetime"
// 9   TestValidator_GetValidationErrors_ReportsFieldMessages     Malformed JSON                              "Invalid JSON syntax"
// 10  TestValidator_FallsBackToTagWithoutTranslator              nil translator, missing required field      {"name": "required"} (raw tag name)
// 11  TestValidator_GetValidationErrors_LocatesUnparsableValues  Non-numeric form body value                 {"count": "count must be integer"}
// 12  TestValidator_GetValidationErrors_LocatesUnparsableValues  Value not found in the request              Generic "invalid value 'abc' for type integer"
// 13  TestValidator_GetValidationErrors_LocatesUnparsableValues  No gin context                              Generic invalid-value message
// 14  TestValidator_GetValidationErrors_LocatesUnparsableValues  ParseBool error                             Message names the boolean type
// 15  TestValidator_GetValidationErrors_LocatesUnparsableValues  ParseFloat error                            Message names the float type
//
// =============================================================================

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jun-develop/junkit/i18n"
	"github.com/jun-develop/junkit/validation"
)

// newValidator returns a Validator built from opts, failing the test on error.
func newValidator(t *testing.T, opts validation.Options) *validation.Validator {
	t.Helper()
	v, err := validation.NewValidator(opts, i18n.NewTranslator())
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	return v
}

// newTestContext returns a gin test context carrying a JSON request with body.
func newTestContext(t *testing.T, method, target, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	return c
}

// assertMapEqual fails the test unless got is a map[string]string equal to want.
func assertMapEqual(t *testing.T, got any, want map[string]string) {
	t.Helper()

	gotMap, ok := got.(map[string]string)
	if !ok {
		t.Fatalf("result type = %T, want map[string]string (value: %v)", got, got)
	}

	if len(gotMap) != len(want) {
		t.Fatalf("GetValidationErrors() = %v, want %v", gotMap, want)
	}

	for k, wantV := range want {
		if gotV, ok := gotMap[k]; !ok || gotV != wantV {
			t.Fatalf("GetValidationErrors()[%q] = %q, want %q", k, gotV, wantV)
		}
	}
}

func TestValidator_GetValidationErrors_ReportsFieldMessages(t *testing.T) {
	v := newValidator(t, validation.Options{})

	t.Run("nil error returns nil", func(t *testing.T) {
		if got := v.GetValidationErrors(nil, nil); got != nil {
			t.Fatalf("GetValidationErrors(nil, nil) = %v, want nil", got)
		}
	})

	t.Run("a missing required field is reported by its snake_case name", func(t *testing.T) {
		type req struct {
			Email string `json:"email" binding:"required,email"`
		}

		ctx := newTestContext(t, http.MethodPost, "/", `{}`)
		var body req
		bindErr := ctx.ShouldBindJSON(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for a missing required field")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		want := map[string]string{"email": "email is required"}
		assertMapEqual(t, got, want)
	})

	t.Run("min on a string field reports a character-count message", func(t *testing.T) {
		type req struct {
			Name string `json:"name" binding:"min=3"`
		}

		ctx := newTestContext(t, http.MethodPost, "/", `{"name":"ab"}`)
		var body req
		bindErr := ctx.ShouldBindJSON(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for a too-short name")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		want := map[string]string{"name": "name must be longer than 3 characters"}
		assertMapEqual(t, got, want)
	})

	t.Run("min on an int field reports a value-comparison message", func(t *testing.T) {
		type req struct {
			Age int `json:"age" binding:"min=18"`
		}

		ctx := newTestContext(t, http.MethodPost, "/", `{"age":5}`)
		var body req
		bindErr := ctx.ShouldBindJSON(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for an under-age value")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		want := map[string]string{"age": "age must be greater than 18"}
		assertMapEqual(t, got, want)
	})

	t.Run("a JSON type mismatch reports the field's expected type", func(t *testing.T) {
		type req struct {
			Age int `json:"age"`
		}

		ctx := newTestContext(t, http.MethodPost, "/", `{"age":"not-a-number"}`)
		var body req
		bindErr := ctx.ShouldBindJSON(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for a string where an int was expected")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		want := map[string]string{"age": "age must be integer"}
		assertMapEqual(t, got, want)
	})

	t.Run("an unparsable query value reports the field's expected type", func(t *testing.T) {
		type req struct {
			Page int `form:"page"`
		}

		ctx := newTestContext(t, http.MethodGet, "/?page=abc", "")
		var body req
		bindErr := ctx.ShouldBindQuery(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for a non-numeric page value")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		want := map[string]string{"page": "page must be integer"}
		assertMapEqual(t, got, want)
	})

	t.Run("an unparsable unsigned query value reports a positive integer", func(t *testing.T) {
		type req struct {
			Limit uint `form:"limit"`
		}

		ctx := newTestContext(t, http.MethodGet, "/?limit=-1", "")
		var body req
		bindErr := ctx.ShouldBindQuery(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for a negative unsigned value")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		want := map[string]string{"limit": "limit must be positive integer"}
		assertMapEqual(t, got, want)
	})

	t.Run("a JSON type error on a *time.Time field uses the datetime name", func(t *testing.T) {
		typeErr := &json.UnmarshalTypeError{Field: "StartAt", Type: reflect.TypeFor[*time.Time]()}

		got := v.GetValidationErrors(nil, typeErr)
		want := map[string]string{"start_at": "start_at must be datetime"}
		assertMapEqual(t, got, want)
	})

	t.Run("malformed JSON reports the generic syntax error message", func(t *testing.T) {
		type req struct {
			Email string `json:"email"`
		}

		ctx := newTestContext(t, http.MethodPost, "/", `{"email":`)
		var body req
		bindErr := ctx.ShouldBindJSON(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for malformed JSON")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		if got != "Invalid JSON syntax" {
			t.Fatalf("GetValidationErrors() = %v, want %q", got, "Invalid JSON syntax")
		}
	})
}

func TestValidator_FallsBackToTagWithoutTranslator(t *testing.T) {
	t.Run("nil translator reports the raw tag name", func(t *testing.T) {
		v, err := validation.NewValidator(validation.Options{}, nil)
		if err != nil {
			t.Fatalf("NewValidator() error = %v", err)
		}

		ctx := newTestContext(t, http.MethodPost, "/", `{}`)
		type req struct {
			Name string `json:"name" binding:"required"`
		}
		var body req
		bindErr := ctx.ShouldBindJSON(&body)
		if bindErr == nil {
			t.Fatal("expected a binding error for a missing required field")
		}

		got := v.GetValidationErrors(ctx, bindErr)
		want := map[string]string{"name": "required"}
		assertMapEqual(t, got, want)
	})
}

func TestValidator_GetValidationErrors_LocatesUnparsableValues(t *testing.T) {
	v := newValidator(t, validation.Options{})

	t.Run("form body value is reported by its form key", func(t *testing.T) {
		type req struct {
			Count int `form:"count"`
		}

		ctx := newTestContext(t, http.MethodPost, "/", "count=abc")
		ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		var body req
		bindErr := ctx.ShouldBind(&body)
		if bindErr == nil {
			t.Fatal("ShouldBind() error = nil, want an error for a non-numeric count")
		}

		got := v.GetValidationErrors(ctx, bindErr)

		assertMapEqual(t, got, map[string]string{"count": "count must be integer"})
	})

	numErr := func(fn, num string) error {
		return &strconv.NumError{Func: fn, Num: num, Err: strconv.ErrSyntax}
	}

	tests := []struct {
		name string
		ctx  *gin.Context
		err  error
		want string
	}{
		{"value not found in the request", newTestContext(t, http.MethodGet, "/?page=1", ""),
			numErr("ParseInt", "abc"), "invalid value 'abc' for type integer"},
		{"no gin context", nil, numErr("ParseInt", "abc"), "invalid value 'abc' for type integer"},
		{"bool parse error", nil, numErr("ParseBool", "maybe"), "invalid value 'maybe' for type boolean"},
		{"float parse error", nil, numErr("ParseFloat", "x"), "invalid value 'x' for type float"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v.GetValidationErrors(tt.ctx, tt.err)

			if got != tt.want {
				t.Errorf("GetValidationErrors() = %#v, want %q", got, tt.want)
			}
		})
	}
}
