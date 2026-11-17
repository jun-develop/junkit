// Package i18n provides a thread-safe translation engine with automatic language fallback.
package i18n

import (
	"fmt"
	"maps"
	"strings"
	"sync"
)

// LangEN and LangVI are the supported language codes; only LangEN has a built-in catalog.
const (
	LangEN = "en"
	LangVI = "vi"
)

// Translator manages per-language message catalogs with thread-safe resolution and fallback.
type Translator struct {
	mu sync.RWMutex

	// translations maps language -> key -> message.
	translations map[string]map[string]string
	defaultLang  string
}

// NewTranslator creates a Translator seeded with the built-in English catalog.
//
// Usage:
//
//	t := i18n.NewTranslator()
//	t.AddTranslations(map[string]map[string]string{
//	    i18n.LangEN: {"user_not_found": "User not found"},
//	})
func NewTranslator() *Translator {
	t := &Translator{
		translations: make(map[string]map[string]string),
		defaultLang:  LangEN,
	}

	t.loadDefaultTranslations()

	return t
}

// SetDefaultLang sets the fallback language; an empty lang resets it to LangEN.
func (t *Translator) SetDefaultLang(lang string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if lang == "" {
		lang = LangEN
	}

	t.defaultLang = lang
}

// GetDefaultLang returns the current default language code.
func (t *Translator) GetDefaultLang() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.defaultLang
}

// AddTranslations merges catalogs into the translator, overwriting existing keys.
//
// Usage:
//
//	t.AddTranslations(map[string]map[string]string{
//	    i18n.LangEN: {"hello": "Hello, %s!"},
//	    i18n.LangVI: {"hello": "Xin chào, %s!"},
//	})
func (t *Translator) AddTranslations(translations map[string]map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for lang, keys := range translations {
		if _, ok := t.translations[lang]; !ok {
			t.translations[lang] = make(map[string]string)
		}

		maps.Copy(t.translations[lang], keys)
	}
}

// GetMessage resolves key in lang (or the default language if empty), falling back to the
// default language and then to key itself. args are applied only if the message has a verb.
//
// Usage:
//
//	msg := t.GetMessage("required", "en", "email")
func (t *Translator) GetMessage(key string, lang string, args ...any) string {
	t.mu.RLock()
	currentLang := lang
	if currentLang == "" {
		currentLang = t.defaultLang
	}

	val, ok := t.translations[currentLang][key]
	if !ok && currentLang != t.defaultLang {
		val, ok = t.translations[t.defaultLang][key]
	}

	t.mu.RUnlock()

	if !ok {
		return key
	}

	if len(args) > 0 && strings.Contains(val, "%") {
		return fmt.Sprintf(val, args...)
	}

	return val
}

// loadDefaultTranslations loads the built-in validation and system catalogs.
func (t *Translator) loadDefaultTranslations() {
	t.loadValidationTranslations()
	t.loadSystemTranslations()
}

// loadValidationTranslations loads the English validation message templates.
func (t *Translator) loadValidationTranslations() {
	t.AddTranslations(map[string]map[string]string{
		LangEN: {
			"validation_error": "Validation error",
			"gt":               "%[1]s must be greater than %[2]s",
			"lt":               "%[1]s must be less than %[2]s",
			"gte":              "%[1]s must be greater than or equal to %[2]s",
			"lte":              "%[1]s must be less than or equal to %[2]s",
			"uuid":             "%[1]s must be a valid UUID",
			"slug":             "%[1]s must only contain lowercase letters, numbers, hyphens, or dots",
			"min":              "%[1]s must be longer than %[2]s characters",
			"max":              "%[1]s must be shorter than %[2]s characters",
			"min_int":          "%[1]s must be greater than %[2]s",
			"max_int":          "%[1]s must be less than %[2]s",
			"oneof":            "%[1]s must be one of the values: %[2]s",
			"required":         "%[1]s is required",
			"search":           "%[1]s must only contain letters, numbers, and spaces",
			"email":            "%[1]s must be a valid email format",
			"date":             "%[1]s must follow the format YYYY-MM-DD",
			"date_past":        "%[1]s must be a date in the past",
			"phone_advanced":   "%[1]s must contain only digits, optionally starting with + and be 7-15 characters long",
			"datetime":         "%[1]s must follow the format YYYY-MM-DD",
			"email_advanced":   "%[1]s is invalid or in the forbidden list",
			"password_strong":  "%[1]s must be at least 8 characters including (lowercase, uppercase, number, and special character)",
			"file_ext":         "%[1]s only allows file extensions: %[2]s",
			"alphanum_dash":    "%[1]s must only contain letters, numbers, hyphens, or underscores",
		},
	})
}

// loadSystemTranslations loads the English system and HTTP message templates.
func (t *Translator) loadSystemTranslations() {
	t.AddTranslations(map[string]map[string]string{
		LangEN: {
			"operation_successful":      "Operation successful",
			"invalid_request":           "Invalid request",
			"invalid_type":              "%[1]s must be %[2]s",
			"invalid_type_value":        "invalid value '%[1]s' for type %[2]s",
			"invalid_json_syntax":       "Invalid JSON syntax",
			"type_string":               "string",
			"type_int":                  "integer",
			"type_int8":                 "integer (8-bit)",
			"type_int16":                "integer (16-bit)",
			"type_int32":                "integer (32-bit)",
			"type_int64":                "integer (64-bit)",
			"type_uint":                 "positive integer",
			"type_uint8":                "positive integer (8-bit)",
			"type_uint16":               "positive integer (16-bit)",
			"type_uint32":               "positive integer (32-bit)",
			"type_uint64":               "positive integer (64-bit)",
			"type_float32":              "float",
			"type_float64":              "float",
			"type_bool":                 "boolean",
			"type_time":                 "datetime",
			"not_found":                 "Not found",
			"api_not_found":             "API is not found",
			"method_not_allowed":        "HTTP method is not allowed for this endpoint",
			"app_running":               "The API is running",
			"missing_id_param":          "Missing 'id' query parameter",
			"invalid_uuid":              "Invalid UUID format",
			"invalid_json":              "Invalid JSON: %[1]s",
			"tx_failed_begin":           "Failed to begin transaction: %[1]s",
			"tx_failed_commit":          "Failed to commit transaction: %[1]s",
			"tx_completed":              "Transaction completed successfully",
			"debug_data_recorded":       "Debug data recorded. Check debug UI.",
			"internal_server_error":     "Internal Server Error",
			"database_connection_error": "Database connection error",
			"unauthorized_token":        "Authentication token is invalid or expired",
			"forbidden_internal_access": "Forbidden internal system access",
			"insufficient_permission":   "You do not have permission to perform this action",
			"too_many_requests":         "Too many requests. Please try again later.",
			"request_body_too_large":    "Request body exceeds the maximum allowed size",
			"access_denied":             "Access denied due to suspicious activity or IP restriction",
		},
	})
}
