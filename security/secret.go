package security

import "strings"

// secretNameHints are lowercase substrings marking a field, parameter, header or DSN key as a
// secret. It is the union of the lists database, tracer and middleware used to keep on their own.
var secretNameHints = []string{
	"password", "passwd", "pwd", "secret", "token",
	"apikey", "api-key", "api_key",
	"authorization", "signature", "session", "cookie",
}

// IsSecretName reports whether name (a field, query parameter, header or DSN key) likely holds a
// secret, by case-insensitive substring match against one shared hint list.
//
// Usage:
//
//	if security.IsSecretName(key) { value = "****" }
func IsSecretName(name string) bool {
	lower := strings.ToLower(name)
	for _, hint := range secretNameHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}

	return false
}

// MaskHeader returns value with any secret it carries, as judged by the header name, hidden:
// Authorization keeps only its scheme, cookies keep only their names, and any other secret-named
// header becomes "****".
func MaskHeader(name, value string) string {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization":
		return maskAuthorization(value)
	case "cookie", "set-cookie":
		return maskCookie(value)
	}

	if IsSecretName(name) {
		return "****"
	}

	return value
}

// maskAuthorization keeps only the auth scheme (e.g. "Bearer") and hides the credentials.
func maskAuthorization(s string) string {
	if s == "" {
		return ""
	}

	if scheme, _, ok := strings.Cut(s, " "); ok {
		return scheme + " ****"
	}

	return "****"
}

// maskCookie replaces every cookie value with "****", keeping the cookie names visible.
func maskCookie(s string) string {
	if s == "" {
		return ""
	}

	parts := strings.Split(s, "; ")
	for i, p := range parts {
		if eq := strings.IndexByte(p, '='); eq > 0 {
			parts[i] = p[:eq+1] + "****"
		}
	}

	return strings.Join(parts, "; ")
}
