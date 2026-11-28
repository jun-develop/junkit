package validation_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: custom_validation_test.go
// Purpose: Verify each custom validation tag registered by RegisterCustomValidation (offline only).
// Type: Unit
//
// Test functions: 13
// Total scenarios: 46
//
// #   Test Case                                        Scenario                           Expected Result
// 1   TestValidator_PasswordStrongTag                  Meets every requirement            Valid
// 2   TestValidator_PasswordStrongTag                  Too short                          Invalid
// 3   TestValidator_PasswordStrongTag                  Missing uppercase                  Invalid
// 4   TestValidator_PasswordStrongTag                  Missing lowercase                  Invalid
// 5   TestValidator_PasswordStrongTag                  Missing digit                      Invalid
// 6   TestValidator_PasswordStrongTag                  Missing special character          Invalid
// 7   TestValidator_SlugTag                            Simple slug                        Valid
// 8   TestValidator_SlugTag                            Dot separator                      Valid
// 9   TestValidator_SlugTag                            Uppercase letters                  Invalid
// 10  TestValidator_SlugTag                            Spaces                             Invalid
// 11  TestValidator_SlugTag                            Leading dash                       Invalid
// 12  TestValidator_SearchTag                          Letters, digits and spaces         Valid
// 13  TestValidator_SearchTag                          Special characters                 Invalid
// 14  TestValidator_MinIntMaxIntTags                   Within range                       Valid
// 15  TestValidator_MinIntMaxIntTags                   At the minimum                     Valid
// 16  TestValidator_MinIntMaxIntTags                   At the maximum                     Valid
// 17  TestValidator_MinIntMaxIntTags                   Below the minimum                  Invalid
// 18  TestValidator_MinIntMaxIntTags                   Above the maximum                  Invalid
// 19  TestValidator_MinIntMaxIntTagsNonIntFields       uint within range                  Valid
// 20  TestValidator_MinIntMaxIntTagsNonIntFields       uint above the maximum             Invalid
// 21  TestValidator_MinIntMaxIntTagsNonIntFields       Tag on a string field              Invalid (no panic)
// 22  TestValidator_MaxIntTagRejectsUnusableParameter  max_int=abc                        Invalid
// 23  TestValidator_MaxIntTagRejectsUnusableParameter  max_int=-1 on a uint field         Invalid (no unsigned value is <= -1)
// 24  TestValidator_FileExtTag                         Allowed extension                  Valid
// 25  TestValidator_FileExtTag                         Allowed extension in upper case    Valid
// 26  TestValidator_FileExtTag                         Disallowed extension               Invalid
// 27  TestValidator_FileExtTag                         No extension                       Invalid
// 28  TestValidator_AlphanumDashTag                    Letters, digits, dash, underscore  Valid
// 29  TestValidator_AlphanumDashTag                    Contains a dot                     Invalid
// 30  TestValidator_DateTags                           date: YYYY-MM-DD                   Valid
// 31  TestValidator_DateTags                           date: other format                 Invalid
// 32  TestValidator_DateTags                           date_past: date before now         Valid
// 33  TestValidator_DateTags                           date_past: future date             Invalid
// 34  TestValidator_PhoneAdvancedTag                   With country code                  Valid
// 35  TestValidator_PhoneAdvancedTag                   Without country code               Valid
// 36  TestValidator_PhoneAdvancedTag                   Too short                          Invalid
// 37  TestValidator_PhoneAdvancedTag                   Contains letters                   Invalid
// 38  TestValidator_EmailAdvancedTag                   Malformed address                  Invalid
// 39  TestValidator_EmailAdvancedTag                   Disposable domain                  Invalid before any DNS lookup
// 40  TestNewValidator_LoadsBlocklistFromPath          Domain listed in the file          Email rejected
// 41  TestNewValidator_LoadsBlocklistFromPath          Upper-case entry in the file       Email rejected (offline smoke test; matching proven in internal test)
// 42  TestNewValidator_LoadsBlocklistFromPath          Blocklist file missing             NewValidator returns os.ErrNotExist
// 43  TestPattern_AgreesWithRule                       RE2-compatible rule patterns       Pattern accepts exactly what the rule accepts
// 44  TestPattern_AgreesWithRule                       email_advanced pattern             Rejects every malformed sample
// 45  TestPattern_AgreesWithRule                       password_strong pattern            Present (lookaheads; checked against JS by hand)
// 46  TestPattern_AgreesWithRule                       Rule without a pattern             ok is false
//
// =============================================================================

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

func TestValidator_MaxIntTagRejectsUnusableParameter(t *testing.T) {
	v := newTestValidator(t)

	type nonNumericParam struct {
		Age int `validate:"max_int=abc"`
	}

	type negativeMaxOnUint struct {
		Count uint `validate:"max_int=-1"`
	}

	tests := []struct {
		name string
		form any
	}{
		{"non-numeric parameter", nonNumericParam{Age: 1}},
		{"negative maximum on an unsigned field", negativeMaxOnUint{Count: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Struct(tt.form); err == nil {
				t.Errorf("Struct(%+v) error = nil, want a max_int validation error", tt.form)
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

func TestNewValidator_LoadsBlocklistFromPath(t *testing.T) {
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
	tests := []struct {
		name  string
		email string
	}{
		{"domain listed in the file", "a@throwaway.example"},
		{"domain listed in upper case", "a@another.example"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := engine.Struct(form{Email: tt.email}); err == nil {
				t.Fatalf("email %q: validate error = nil, want the blocklisted domain rejected", tt.email)
			}
		})
	}

	t.Run("blocklist file missing", func(t *testing.T) {
		_, err := validation.NewValidator(validation.Options{BlocklistPath: filepath.Join(dir, "missing.conf")}, nil)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("NewValidator() error = %v, want os.ErrNotExist", err)
		}
	})
}

func TestPattern_AgreesWithRule(t *testing.T) {
	v := newTestValidator(t)
	samples := []string{
		"hello-world", "v1.2", "Hello World", "-x", "abc_DEF-1", "a.b", "", " ", "x y z",
		"+84901234567", "0901234567", "12345", "090-123", "+", "abc", "1234567890123456",
	}

	for _, rule := range []string{"slug", "search", "alphanum_dash", "phone_advanced"} {
		t.Run(rule, func(t *testing.T) {
			pattern, ok := validation.Pattern(rule)
			if !ok {
				t.Fatalf("Pattern(%q) ok = false", rule)
			}

			re := regexp.MustCompile(pattern)
			for _, s := range samples {
				if s == "" {
					continue // the rules never see an empty value behind omitempty
				}

				if got, want := re.MatchString(s), v.Var(s, rule) == nil; got != want {
					t.Errorf("%q: pattern match = %v, rule ok = %v", s, got, want)
				}
			}
		})
	}

	t.Run("email_advanced rejects malformed addresses", func(t *testing.T) {
		pattern, _ := validation.Pattern("email_advanced")
		re := regexp.MustCompile(pattern)
		for _, s := range []string{"plain", "a@", "@b.com", "a b@c.com", "a@-b.com"} {
			if re.MatchString(s) {
				t.Errorf("%q matched the email pattern", s)
			}
		}

		if !re.MatchString("first.last+tag@mail.example.com") {
			t.Error("a valid address did not match the email pattern")
		}
	})

	t.Run("password_strong has a pattern", func(t *testing.T) {
		if _, ok := validation.Pattern("password_strong"); !ok {
			t.Fatal("Pattern(password_strong) ok = false")
		}
	})

	t.Run("rule without a pattern", func(t *testing.T) {
		if _, ok := validation.Pattern("min_int"); ok {
			t.Fatal("Pattern(min_int) ok = true")
		}
	})
}
