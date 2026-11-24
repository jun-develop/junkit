// Package stringx holds string helpers the standard strings package lacks.
package stringx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ToSnake converts a PascalCase or camelCase identifier to snake_case, keeping an acronym in one
// word.
//
// Usage:
//
//	stringx.ToSnake("AvatarURL") // "avatar_url"
//	stringx.ToSnake("APIKey")    // "api_key"
func ToSnake(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || nextLower && unicode.IsUpper(prev) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}

	return b.String()
}

// ToPascal converts a snake_case string to PascalCase.
//
// Usage:
//
//	stringx.ToPascal("order_item") // "OrderItem"
func ToPascal(s string) string {
	var b strings.Builder
	for _, part := range strings.Split(s, "_") {
		b.WriteString(UpperFirst(part))
	}

	return b.String()
}

// ToCamel converts a snake_case string to camelCase.
//
// Usage:
//
//	stringx.ToCamel("order_item") // "orderItem"
func ToCamel(s string) string {
	return LowerFirst(ToPascal(s))
}

// UpperFirst returns s with its first rune upper-cased.
//
// Usage:
//
//	stringx.UpperFirst("đẹp trai") // "Đẹp trai"
func UpperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 || unicode.IsUpper(r) {
		return s
	}

	return string(unicode.ToUpper(r)) + s[size:]
}

// LowerFirst returns s with its first rune lower-cased.
//
// Usage:
//
//	stringx.LowerFirst("Hello") // "hello"
func LowerFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 || unicode.IsLower(r) {
		return s
	}

	return string(unicode.ToLower(r)) + s[size:]
}
