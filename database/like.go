package database

import "strings"

// likeEscaper escapes LIKE's wildcards and its default escape character, backslash.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// EscapeLike makes s match literally inside a LIKE / ILIKE pattern, so a search for "50%" or "a_b"
// does not turn % and _ into wildcards.
//
// Usage:
//
//	rows, err := q.ListUsers(ctx, sqlc.ListUsersParams{Search: database.EscapeLike(filter.Search)})
func EscapeLike(s string) string {
	return likeEscaper.Replace(s)
}
