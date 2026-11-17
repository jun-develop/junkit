package validation_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-playground/validator/v10"

	"github.com/jun-develop/junkit/validation"
)

// newTestValidator returns a validator.Validate with the custom rules registered.
func newTestValidator(t *testing.T) *validator.Validate {
	t.Helper()
	engine := validator.New()
	if err := newValidator(t, validation.Options{}).RegisterCustomValidation(engine); err != nil {
		t.Fatalf("RegisterCustomValidation() error = %v", err)
	}

	return engine
}

func TestValidator_PasswordStrongTag(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Password string `validate:"password_strong"`
	}

	tests := []struct {
		name     string
		password string
		wantOK   bool
	}{
		{name: "meets every requirement", password: "Abcdef1!", wantOK: true},
		{name: "too short", password: "Ab1!", wantOK: false},
		{name: "missing uppercase", password: "abcdef1!", wantOK: false},
		{name: "missing lowercase", password: "ABCDEF1!", wantOK: false},
		{name: "missing digit", password: "Abcdefg!", wantOK: false},
		{name: "missing special character", password: "Abcdefg1", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Password: tt.password})
			if (err == nil) != tt.wantOK {
				t.Fatalf("password %q: validate error = %v, want ok=%v", tt.password, err, tt.wantOK)
			}
		})
	}
}

func TestValidator_SlugTag(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Slug string `validate:"slug"`
	}

	tests := []struct {
		name   string
		slug   string
		wantOK bool
	}{
		{name: "simple slug", slug: "hello-world", wantOK: true},
		{name: "dot separator is allowed", slug: "v1.2", wantOK: true},
		{name: "uppercase is rejected", slug: "Hello-World", wantOK: false},
		{name: "spaces are rejected", slug: "hello world", wantOK: false},
		{name: "leading dash is rejected", slug: "-hello", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Slug: tt.slug})
			if (err == nil) != tt.wantOK {
				t.Fatalf("slug %q: validate error = %v, want ok=%v", tt.slug, err, tt.wantOK)
			}
		})
	}
}

func TestValidator_SearchTag(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Search string `validate:"search"`
	}

	tests := []struct {
		name   string
		search string
		wantOK bool
	}{
		{name: "letters and digits", search: "John 2nd", wantOK: true},
		{name: "special characters are rejected", search: "John; DROP TABLE", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Search: tt.search})
			if (err == nil) != tt.wantOK {
				t.Fatalf("search %q: validate error = %v, want ok=%v", tt.search, err, tt.wantOK)
			}
		})
	}
}

func TestValidator_MinIntMaxIntTags(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Age int `validate:"min_int=1,max_int=130"`
	}

	tests := []struct {
		name   string
		age    int
		wantOK bool
	}{
		{name: "within range", age: 30, wantOK: true},
		{name: "at the minimum", age: 1, wantOK: true},
		{name: "at the maximum", age: 130, wantOK: true},
		{name: "below the minimum", age: 0, wantOK: false},
		{name: "above the maximum", age: 131, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Age: tt.age})
			if (err == nil) != tt.wantOK {
				t.Fatalf("age %d: validate error = %v, want ok=%v", tt.age, err, tt.wantOK)
			}
		})
	}
}

func TestValidator_MinIntMaxIntTagsNonIntFields(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Count uint8  `validate:"min_int=1,max_int=10"`
		Name  string `validate:"omitempty,min_int=1"`
	}

	tests := []struct {
		name   string
		form   form
		wantOK bool
	}{
		{name: "uint within range", form: form{Count: 5}, wantOK: true},
		{name: "uint above the maximum", form: form{Count: 11}, wantOK: false},
		{name: "string field is rejected instead of panicking", form: form{Count: 5, Name: "x"}, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Struct(tt.form); (err == nil) != tt.wantOK {
				t.Fatalf("validate error = %v, want ok=%v", err, tt.wantOK)
			}
		})
	}
}

func TestValidator_FileExtTag(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Filename string `validate:"file_ext=jpg png"`
	}

	tests := []struct {
		name     string
		filename string
		wantOK   bool
	}{
		{name: "allowed extension", filename: "avatar.jpg", wantOK: true},
		{name: "allowed extension, case-insensitive", filename: "avatar.PNG", wantOK: true},
		{name: "disallowed extension", filename: "script.exe", wantOK: false},
		{name: "no extension", filename: "avatar", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Filename: tt.filename})
			if (err == nil) != tt.wantOK {
				t.Fatalf("filename %q: validate error = %v, want ok=%v", tt.filename, err, tt.wantOK)
			}
		})
	}
}

func TestValidator_AlphanumDashTag(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Code string `validate:"alphanum_dash"`
	}

	tests := []struct {
		name   string
		code   string
		wantOK bool
	}{
		{name: "letters, digits, dash, underscore", code: "SKU_123-a", wantOK: true},
		{name: "a dot is rejected", code: "SKU.123", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Code: tt.code})
			if (err == nil) != tt.wantOK {
				t.Fatalf("code %q: validate error = %v, want ok=%v", tt.code, err, tt.wantOK)
			}
		})
	}
}

func TestValidator_DateTags(t *testing.T) {
	v := newTestValidator(t)

	type dateForm struct {
		Date string `validate:"date"`
	}
	type datePastForm struct {
		Date string `validate:"date_past"`
	}

	tests := []struct {
		name   string
		form   any
		wantOK bool
	}{
		{name: "date accepts YYYY-MM-DD", form: dateForm{Date: "2026-01-15"}, wantOK: true},
		{name: "date rejects other formats", form: dateForm{Date: "15/01/2026"}, wantOK: false},
		{name: "date_past accepts a date before now", form: datePastForm{Date: "2020-01-01"}, wantOK: true},
		{name: "date_past rejects a future date", form: datePastForm{Date: "2999-01-01"}, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Struct(tt.form); (err == nil) != tt.wantOK {
				t.Fatalf("validate error = %v, want ok=%v", err, tt.wantOK)
			}
		})
	}
}

func TestValidator_PhoneAdvancedTag(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Phone string `validate:"phone_advanced"`
	}

	tests := []struct {
		name   string
		phone  string
		wantOK bool
	}{
		{name: "with country code", phone: "+84912345678", wantOK: true},
		{name: "without country code", phone: "0912345678", wantOK: true},
		{name: "too short", phone: "12345", wantOK: false},
		{name: "contains letters", phone: "091234abcd", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Phone: tt.phone})
			if (err == nil) != tt.wantOK {
				t.Fatalf("phone %q: validate error = %v, want ok=%v", tt.phone, err, tt.wantOK)
			}
		})
	}
}

// TestValidator_EmailAdvancedTag covers format and disposable-domain checks without network I/O.
func TestValidator_EmailAdvancedTag(t *testing.T) {
	v := newTestValidator(t)

	type form struct {
		Email string `validate:"email_advanced"`
	}

	tests := []struct {
		name   string
		email  string
		wantOK bool
	}{
		{name: "malformed address", email: "not-an-email", wantOK: false},
		{name: "disposable domain is rejected before any network lookup", email: "user@mailinator.com", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Struct(form{Email: tt.email})
			if (err == nil) != tt.wantOK {
				t.Fatalf("email %q: validate error = %v, want ok=%v", tt.email, err, tt.wantOK)
			}
		})
	}
}

func TestNewValidator_BlocklistPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocklist.conf")
	if err := os.WriteFile(path, []byte("throwaway.example\n\nANOTHER.example\n"), 0o644); err != nil {
		t.Fatalf("write blocklist: %v", err)
	}

	type form struct {
		Email string `validate:"email_advanced"`
	}

	engine := validator.New()
	if err := newValidator(t, validation.Options{BlocklistPath: path}).RegisterCustomValidation(engine); err != nil {
		t.Fatalf("RegisterCustomValidation() error = %v", err)
	}

	// Listed domains are rejected before any DNS lookup, so these stay offline.
	for _, email := range []string{"a@throwaway.example", "a@another.example"} {
		t.Run(email, func(t *testing.T) {
			if err := engine.Struct(form{Email: email}); err == nil {
				t.Fatalf("email %q: validate error = nil, want the blocklisted domain rejected", email)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		_, err := validation.NewValidator(validation.Options{BlocklistPath: filepath.Join(dir, "missing.conf")}, nil)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("NewValidator() error = %v, want os.ErrNotExist", err)
		}
	})
}
