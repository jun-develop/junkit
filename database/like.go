package database

import "strings"

// likeEscaper escapes LIKE's wildcards and its default escape character, backslash.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// EscapeLike escapes wildcards ('%', '_', '\') so s matches literally in LIKE/ILIKE queries.
//
// Usage:
//
//	rows, err := q.ListUsers(ctx, sqlc.ListUsersParams{Search: database.EscapeLike(filter.Search)})
func EscapeLike(s string) string {
	return likeEscaper.Replace(s)
}
