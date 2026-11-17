package database

import "regexp"

// sensitiveRe matches "key=value" pairs whose key commonly holds a secret (password, token, api
// key, ...). A quoted value may contain backslash-escaped quotes, as libpq allows.
var sensitiveRe = regexp.MustCompile(
	`(?i)(\b(password|passwd|pwd|secret|token|api[_-]?key)\s*=\s*)('(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"|\S+)`,
)

// urlPasswordRe matches the password in a URL's userinfo ("scheme://user:password@"). It runs up
// to the last "@" before the path, so an unescaped "@" in the password is masked too.
var urlPasswordRe = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://[^:/@\s]*:)[^/\s]*@`)

// MaskDSN redacts secrets in a connection string with "********", in both the key=value form
// ("password=secret") and the URL form ("postgres://user:secret@host/db").
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
