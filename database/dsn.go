package database

import "regexp"

// sensitiveRe matches sensitive key-value pairs (e.g. password=..., token=...).
var sensitiveRe = regexp.MustCompile(`(?i)(\b(password|passwd|pwd|secret|token|api[_-]?key)\s*=\s*)('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"|\S+)`)

// urlPasswordRe matches the password in URLs (e.g. scheme://user:password@).
var urlPasswordRe = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^:/@\s]*:)[^/\s]*@`)

// MaskDSN replaces secrets in a DSN or connection URL with "********".
//
// Usage:
//
//	log.Info("connecting", zap.String("dsn", database.MaskDSN(dsn)))
func MaskDSN(dsn string) string {
	if dsn == "" {
		return dsn
	}

	dsn = urlPasswordRe.ReplaceAllString(dsn, `${1}********@`)

	return sensitiveRe.ReplaceAllString(dsn, `${1}********`)
}
