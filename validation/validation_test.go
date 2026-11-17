package validation_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestValidator_GetValidationErrors(t *testing.T) {
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

func TestValidator_Translate(t *testing.T) {
	t.Run("a nil translator falls back to the given default", func(t *testing.T) {
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

		got := v.GetValidationErrors(ctx, bindErr)
		// The fallback is the raw tag name.
		want := map[string]string{"name": "required"}
		assertMapEqual(t, got, want)
	})
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
