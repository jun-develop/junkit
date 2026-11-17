package sqlfmt

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestFormatValue_RendersLiterals(t *testing.T) {
	sampleTime := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	sampleUUID := uuid.MustParse("8040c036-b0a5-4178-8015-c4c4c6b37164")

	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			name: "nil becomes NULL",
			v:    nil,
			want: "NULL",
		},
		{
			name: "nil pointer becomes NULL",
			v:    (*string)(nil),
			want: "NULL",
		},
		{
			name: "pointer is dereferenced",
			v:    func() *string { s := "hello"; return &s }(),
			want: "'hello'",
		},
		{
			name: "string is single-quoted",
			v:    "hello",
			want: "'hello'",
		},
		{
			name: "a single quote inside a string is escaped by doubling",
			v:    "O'Brien",
			want: "'O''Brien'",
		},
		{
			name: "bool true",
			v:    true,
			want: "true",
		},
		{
			name: "bool false",
			v:    false,
			want: "false",
		},
		{
			name: "time.Time is formatted as RFC3339",
			v:    sampleTime,
			want: "'2026-01-15T10:30:00Z'",
		},
		{
			name: "uuid.UUID is quoted",
			v:    sampleUUID,
			want: "'8040c036-b0a5-4178-8015-c4c4c6b37164'",
		},
		{
			name: "int",
			v:    42,
			want: "42",
		},
		{
			name: "int64",
			v:    int64(-7),
			want: "-7",
		},
		{
			name: "float64",
			v:    3.5,
			want: "3.5",
		},
		{
			name: "nil slice becomes NULL",
			v:    []int(nil),
			want: "NULL",
		},
		{
			name: "empty non-nil slice becomes an empty array",
			v:    []int{},
			want: "'{}'",
		},
		{
			name: "string slice elements are double-quoted and escaped",
			v:    []string{"a", `say "hi"`, `back\slash`, "it's"},
			want: `'{"a","say \"hi\"","back\\slash","it''s"}'`,
		},
		{
			name: "nil element pointer becomes a bare NULL",
			v:    []*string{nil},
			want: "'{NULL}'",
		},
		{
			name: "nested slices become a multidimensional array",
			v:    [][]int{{1, 2}, {3, 4}},
			want: "'{{1,2},{3,4}}'",
		},
		{
			name: "int slice becomes a Postgres array literal",
			v:    []int{1, 2, 3},
			want: "'{1,2,3}'",
		},
		{
			name: "a type with no dedicated case falls back to %v, quoted",
			v:    struct{ X int }{X: 1},
			want: "'{1}'",
		},

		// pgtype wrappers used by generated sqlc code
		{
			name: "valid pgtype.UUID",
			v:    pgtype.UUID{Bytes: sampleUUID, Valid: true},
			want: "'8040c036-b0a5-4178-8015-c4c4c6b37164'",
		},
		{
			name: "invalid pgtype.UUID is NULL",
			v:    pgtype.UUID{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Text",
			v:    pgtype.Text{String: "hi", Valid: true},
			want: "'hi'",
		},
		{
			name: "invalid pgtype.Text is NULL",
			v:    pgtype.Text{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Int8",
			v:    pgtype.Int8{Int64: 9, Valid: true},
			want: "9",
		},
		{
			name: "invalid pgtype.Int8 is NULL",
			v:    pgtype.Int8{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Int4",
			v:    pgtype.Int4{Int32: 9, Valid: true},
			want: "9",
		},
		{
			name: "invalid pgtype.Int4 is NULL",
			v:    pgtype.Int4{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Int2",
			v:    pgtype.Int2{Int16: 9, Valid: true},
			want: "9",
		},
		{
			name: "invalid pgtype.Int2 is NULL",
			v:    pgtype.Int2{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Bool",
			v:    pgtype.Bool{Bool: true, Valid: true},
			want: "true",
		},
		{
			name: "invalid pgtype.Bool is NULL",
			v:    pgtype.Bool{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Timestamptz",
			v:    pgtype.Timestamptz{Time: sampleTime, Valid: true},
			want: "'2026-01-15T10:30:00Z'",
		},
		{
			name: "invalid pgtype.Timestamptz is NULL",
			v:    pgtype.Timestamptz{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Timestamp",
			v:    pgtype.Timestamp{Time: sampleTime, Valid: true},
			want: "'2026-01-15 10:30:00'",
		},
		{
			name: "invalid pgtype.Timestamp is NULL",
			v:    pgtype.Timestamp{Valid: false},
			want: "NULL",
		},
		{
			name: "valid pgtype.Date",
			v:    pgtype.Date{Time: sampleTime, Valid: true},
			want: "'2026-01-15'",
		},
		{
			name: "invalid pgtype.Date is NULL",
			v:    pgtype.Date{Valid: false},
			want: "NULL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatValue(tt.v); got != tt.want {
				t.Fatalf("FormatValue(%#v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestBind_ReplacesPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		args []any
		want string
	}{
		{
			name: "no args returns sql unchanged",
			sql:  "SELECT * FROM users WHERE uuid = $1",
			args: nil,
			want: "SELECT * FROM users WHERE uuid = $1",
		},
		{
			name: "a single placeholder is replaced",
			sql:  "SELECT * FROM users WHERE email = $1",
			args: []any{"jane@example.com"},
			want: "SELECT * FROM users WHERE email = 'jane@example.com'",
		},
		{
			name: "multiple placeholders are each replaced",
			sql:  "INSERT INTO users (email, status) VALUES ($1, $2)",
			args: []any{"jane@example.com", 1},
			want: "INSERT INTO users (email, status) VALUES ('jane@example.com', 1)",
		},
		{
			name: "double-digit placeholders are parsed correctly",
			sql:  "SELECT $10, $1",
			args: []any{"first", "second", "third", "fourth", "fifth", "sixth", "seventh", "eighth", "ninth", "tenth"},
			want: "SELECT 'tenth', 'first'",
		},
		{
			name: "$0 is not a valid placeholder and is left as-is",
			sql:  "SELECT $0",
			args: []any{"x"},
			want: "SELECT $0",
		},
		{
			name: "an out-of-range placeholder is left as-is",
			sql:  "SELECT $1, $5",
			args: []any{"only one arg"},
			want: "SELECT 'only one arg', $5",
		},
		{
			name: "a $ not followed by a digit is left as-is",
			sql:  "SELECT price_tag FROM t WHERE note = 'costs $ a lot'",
			args: []any{"x"},
			want: "SELECT price_tag FROM t WHERE note = 'costs $ a lot'",
		},
		{
			name: "a $N inside a string literal is not a placeholder",
			sql:  "SELECT 'costs $1', 'it''s $2' FROM t WHERE id = $1",
			args: []any{7, 8},
			want: "SELECT 'costs $1', 'it''s $2' FROM t WHERE id = 7",
		},
		{
			name: "a $N inside a $$ body is not a placeholder",
			sql:  "DO $$ BEGIN PERFORM $1; END $$; SELECT $1",
			args: []any{7},
			want: "DO $$ BEGIN PERFORM $1; END $$; SELECT 7",
		},
		{
			name: "a $N inside a tagged dollar-quoted body is not a placeholder",
			sql:  "SELECT $fn$ x = $1 $$ $fn$, $1",
			args: []any{7},
			want: "SELECT $fn$ x = $1 $$ $fn$, 7",
		},
		{
			name: "an unterminated string literal is copied to the end",
			sql:  "SELECT $1, 'oops $1",
			args: []any{7},
			want: "SELECT 7, 'oops $1",
		},
		{
			name: "a placeholder right after a string literal is replaced",
			sql:  "SELECT 'a'||$1",
			args: []any{"b"},
			want: "SELECT 'a'||'b'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Bind(tt.sql, tt.args); got != tt.want {
				t.Fatalf("Bind(%q, %v) = %q, want %q", tt.sql, tt.args, got, tt.want)
			}
		})
	}
}

func TestGuessName_UsesLeadingKeyword(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "select", sql: "SELECT * FROM users", want: "Select"},
		{name: "lowercase keyword is still recognized", sql: "select * from users", want: "Select"},
		{name: "insert", sql: "INSERT INTO users (email) VALUES ($1)", want: "Insert"},
		{name: "update", sql: "UPDATE users SET email = $1", want: "Update"},
		{name: "delete", sql: "DELETE FROM users", want: "Delete"},
		{name: "with (CTE)", sql: "WITH recent AS (SELECT 1) SELECT * FROM recent", want: "With"},
		{name: "unrecognized keyword falls back to Query", sql: "BEGIN", want: "Query"},
		{name: "empty string falls back to Query", sql: "", want: "Query"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessName(tt.sql); got != tt.want {
				t.Fatalf("guessName(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestParse_ExtractsAnnotation(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want Info
	}{
		{
			name: "a sqlc name annotation is parsed",
			sql:  "-- name: GetUserByUUID :one\nSELECT * FROM users WHERE uuid = $1",
			want: Info{Name: "GetUserByUUID", Operation: "ONE", SQL: "SELECT * FROM users WHERE uuid = $1"},
		},
		{
			name: "no annotation falls back to a guessed name",
			sql:  "SELECT * FROM users",
			want: Info{Name: "Select", SQL: "SELECT * FROM users"},
		},
		{
			name: "comments and extra whitespace are stripped",
			sql:  "SELECT *\n  -- this is a comment\n  FROM   users\nWHERE  id = 1",
			want: Info{Name: "Select", SQL: "SELECT * FROM users WHERE id = 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.sql); got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestParse_CacheIsBounded(t *testing.T) {
	for i := range maxCached + 50 {
		Parse(fmt.Sprintf("SELECT %d", i))
	}

	if n := cacheSize.Load(); n > maxCached {
		t.Fatalf("cacheSize = %d, want at most %d", n, maxCached)
	}

	// Queries past the cap are still parsed correctly, just not cached.
	if got := Parse("SELECT 'uncached'"); got.Name != "Select" {
		t.Fatalf("Parse past the cap = %+v, want Name=Select", got)
	}
}

func TestBindQuestion_ReplacesPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		args []any
		want string
	}{
		{
			name: "no args returns sql unchanged",
			sql:  "SELECT * FROM users WHERE id = ?",
			args: nil,
			want: "SELECT * FROM users WHERE id = ?",
		},
		{
			name: "placeholders are replaced in order",
			sql:  "INSERT INTO users (email, status) VALUES (?, ?)",
			args: []any{"jane@example.com", 1},
			want: "INSERT INTO users (email, status) VALUES ('jane@example.com', 1)",
		},
		{
			name: "a ? inside a string literal is not a placeholder",
			sql:  "SELECT 'why?', 'it''s?' FROM users WHERE id = ?",
			args: []any{7},
			want: "SELECT 'why?', 'it''s?' FROM users WHERE id = 7",
		},
		{
			name: "placeholders beyond the args are left as-is",
			sql:  "SELECT ?, ?",
			args: []any{"only one arg"},
			want: "SELECT 'only one arg', ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BindQuestion(tt.sql, tt.args); got != tt.want {
				t.Fatalf("BindQuestion() = %q, want %q", got, tt.want)
			}
		})
	}
}
