package tracer_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: db_test.go
// Purpose: Verify that the pgx tracing wrapper and BeginQuery record SQL entries on the Debugger.
// Type: Unit (pgx is replaced by the in-memory fakeDB / fakeRows below)
//
// Test functions: 5
// Total scenarios: 9
//
// #  Test Case                                    Scenario                                Expected Result
// 1  TestPgxWrapper_PassesThroughWithoutDebugger  Query without a Debugger                Returns the inner rows unwrapped
// 2  TestPgxWrapper_ExecRecordsStatement          Annotated UPDATE with one argument      One TouchUsers entry with bound SQL, EXEC operation, 3 rows affected
// 3  TestPgxWrapper_QueryRecordsRowsOnceOnClose   Two rows read, Close called twice       One entry, RowCount 2, JSON bytes kept as JSON and others as hex
// 4  TestPgxWrapper_QueryRowRecordsResult         One row scanned                         Entry has the scanned row and the bound SQL
// 5  TestPgxWrapper_QueryRowRecordsResult         No rows                                 Scan returns pgx.ErrNoRows; entry records it
// 6  TestPgxWrapper_QueryRowRecordsResult         Query fails                             Scan returns the error; entry records it with this test as origin
// 7  TestPgxWrapper_QueryRowRecordsResult         Successful query                        Entry has no origin location
// 8  TestBeginQuery_SupportsNonPgxBind            No Debugger                             Returns nil
// 9  TestBeginQuery_SupportsNonPgxBind            ? placeholder with sqlfmt.BindQuestion  One GetUser entry with the placeholder bound
//
// =============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jun-develop/junkit/sqlfmt"
	"github.com/jun-develop/junkit/tracer"
)

// fakeDB is a DBTX whose Query returns the configured rows (or error).
type fakeDB struct {
	cols     []string
	rows     [][]any
	queryErr error
}

// Exec reports three rows updated.
func (f *fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 3"), nil
}

// Query returns the configured rows, or queryErr.
func (f *fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}

	return &fakeRows{cols: f.cols, rows: f.rows, i: -1}, nil
}

// QueryRow panics; the wrapper must route QueryRow through Query.
func (f *fakeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	panic("the tracing wrapper should route QueryRow through Query")
}

// fakeRows implements pgx.Rows over in-memory values; Scan supports *int, *string and *[]byte.
type fakeRows struct {
	cols []string
	rows [][]any
	i    int
}

// Close is a no-op.
func (r *fakeRows) Close() {}

// Err always reports success.
func (r *fakeRows) Err() error { return nil }

// CommandTag returns an empty tag.
func (r *fakeRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

// Next advances to the following row.
func (r *fakeRows) Next() bool { r.i++; return r.i < len(r.rows) }

// Values returns the current row.
func (r *fakeRows) Values() ([]any, error) { return r.rows[r.i], nil }

// RawValues is unused by the wrapper.
func (r *fakeRows) RawValues() [][]byte { return nil }

// Conn is unused by the wrapper.
func (r *fakeRows) Conn() *pgx.Conn { return nil }

// TypeMap is unused by the wrapper.
func (r *fakeRows) TypeMap() *pgtype.Map { return nil }

// FieldDescriptions describes the configured columns.
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	fds := make([]pgconn.FieldDescription, len(r.cols))
	for i, c := range r.cols {
		fds[i].Name = c
	}

	return fds
}

// Scan copies the current row into *int, *string or *[]byte destinations.
func (r *fakeRows) Scan(dest ...any) error {
	for i, d := range dest {
		switch p := d.(type) {
		case *int:
			*p = r.rows[r.i][i].(int)
		case *string:
			*p = r.rows[r.i][i].(string)
		case *[]byte:
			*p = r.rows[r.i][i].([]byte)
		}
	}

	return nil
}

// sqlEntries returns d's entries, asserting they are all SQL entries.
func sqlEntries(t *testing.T, d *tracer.Debugger) []tracer.LogEntry {
	t.Helper()
	logs := d.Logs()
	for _, e := range logs {
		if e.Type != tracer.TypeSQL {
			t.Fatalf("entry %+v is not a SQL entry", e)
		}
	}

	return logs
}

// onlySQLData fails unless d holds exactly one SQL entry, and returns that entry's data.
func onlySQLData(t *testing.T, d *tracer.Debugger) tracer.SQLData {
	t.Helper()
	logs := sqlEntries(t, d)
	if len(logs) != 1 {
		t.Fatalf("got %d SQL entries, want 1", len(logs))
	}

	return logs[0].Data.(tracer.SQLData)
}

func TestPgxWrapper_PassesThroughWithoutDebugger(t *testing.T) {
	db := tracer.NewPgxWrapper(&fakeDB{cols: []string{"id"}, rows: [][]any{{1}}})

	rows, err := db.Query(context.Background(), "SELECT id FROM users")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if _, traced := rows.(*fakeRows); !traced {
		t.Fatalf("Query without a Debugger returned %T, want the inner rows unwrapped", rows)
	}
}

func TestPgxWrapper_ExecRecordsStatement(t *testing.T) {
	ctx, d := newCtx()
	db := tracer.NewPgxWrapper(&fakeDB{})

	if _, err := db.Exec(ctx, "-- name: TouchUsers :exec\nUPDATE users SET seen = $1", true); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	logs := sqlEntries(t, d)
	if len(logs) != 1 || logs[0].Label != "TouchUsers" {
		t.Fatalf("Logs() = %+v, want one TouchUsers entry", logs)
	}

	data := logs[0].Data.(tracer.SQLData)
	if data.SQL != "UPDATE users SET seen = true" {
		t.Errorf("SQL = %q, want the bound SQL %q", data.SQL, "UPDATE users SET seen = true")
	}

	if data.Operation != "EXEC" {
		t.Errorf("Operation = %q, want %q", data.Operation, "EXEC")
	}

	if data.RowsAffected == nil || *data.RowsAffected != 3 {
		t.Errorf("RowsAffected = %v, want 3", data.RowsAffected)
	}
}

func TestPgxWrapper_QueryRecordsRowsOnceOnClose(t *testing.T) {
	ctx, d := newCtx()
	db := tracer.NewPgxWrapper(&fakeDB{
		cols: []string{"id", "meta"},
		rows: [][]any{{1, []byte(`{"a":1}`)}, {2, []byte{0xde, 0xad}}},
	})

	rows, err := db.Query(ctx, "SELECT id, meta FROM users")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	for rows.Next() {
		var id int
		var meta []byte
		if err := rows.Scan(&id, &meta); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
	}

	rows.Close()
	// An explicit Close plus a deferred one must still record once.
	rows.Close()

	logs := sqlEntries(t, d)
	if len(logs) != 1 {
		t.Fatalf("got %d SQL entries, want exactly 1 even when Close is called twice", len(logs))
	}

	data := logs[0].Data.(tracer.SQLData)
	if data.RowCount == nil || *data.RowCount != 2 {
		t.Fatalf("RowCount = %v, want 2", data.RowCount)
	}

	got, err := json.Marshal(data.Rows)
	if err != nil {
		t.Fatalf("json.Marshal(Rows) error = %v", err)
	}

	if want := `[{"id":1,"meta":{"a":1}},{"id":2,"meta":"dead"}]`; string(got) != want {
		t.Errorf("Rows = %s, want %s (JSON bytes kept as JSON, others as hex)", got, want)
	}
}

func TestPgxWrapper_QueryRowRecordsResult(t *testing.T) {
	t.Run("records the scanned row", func(t *testing.T) {
		ctx, d := newCtx()
		db := tracer.NewPgxWrapper(&fakeDB{cols: []string{"email"}, rows: [][]any{{"jane@example.com"}}})

		var email string
		if err := db.QueryRow(ctx, "SELECT email FROM users WHERE id = $1", 7).Scan(&email); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		data := onlySQLData(t, d)
		if data.Result["email"] != "jane@example.com" {
			t.Errorf("Result = %v, want the scanned email", data.Result)
		}

		if data.SQL != "SELECT email FROM users WHERE id = 7" {
			t.Errorf("SQL = %q, want the bound SQL", data.SQL)
		}
	})

	t.Run("no rows is recorded as ErrNoRows", func(t *testing.T) {
		ctx, d := newCtx()
		db := tracer.NewPgxWrapper(&fakeDB{cols: []string{"email"}})

		var email string
		if err := db.QueryRow(ctx, "SELECT email FROM users").Scan(&email); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("Scan error = %v, want pgx.ErrNoRows", err)
		}

		if data := onlySQLData(t, d); data.Error != pgx.ErrNoRows.Error() {
			t.Errorf("Error = %q, want %q", data.Error, pgx.ErrNoRows.Error())
		}
	})

	t.Run("a query error is returned from Scan and recorded", func(t *testing.T) {
		ctx, d := newCtx()
		boom := errors.New("connection refused")
		db := tracer.NewPgxWrapper(&fakeDB{queryErr: boom})

		if err := db.QueryRow(ctx, "SELECT 1").Scan(); !errors.Is(err, boom) {
			t.Fatalf("Scan error = %v, want %v", err, boom)
		}

		data := onlySQLData(t, d)
		if data.Error != boom.Error() {
			t.Errorf("Error = %q, want %q", data.Error, boom.Error())
		}

		// The origin is the code that ran the query, not the tracer wrapper.
		if !strings.Contains(data.Location, "db_test.go:") {
			t.Errorf("Location = %q, want this test file (db_test.go)", data.Location)
		}

		if !strings.Contains(data.Function, "TestPgxWrapper_QueryRowRecordsResult") {
			t.Errorf("Function = %q, want this test function", data.Function)
		}
	})

	t.Run("a successful query has no origin", func(t *testing.T) {
		ctx, d := newCtx()
		db := tracer.NewPgxWrapper(&fakeDB{cols: []string{"email"}, rows: [][]any{{"a@b.c"}}})

		var email string
		if err := db.QueryRow(ctx, "SELECT email FROM users").Scan(&email); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}

		if data := onlySQLData(t, d); data.Location != "" {
			t.Errorf("Location = %q, want empty for a successful query", data.Location)
		}
	})
}

func TestBeginQuery_SupportsNonPgxBind(t *testing.T) {
	t.Run("no debugger", func(t *testing.T) {
		if q := tracer.BeginQuery(context.Background(), "SELECT 1", nil, sqlfmt.Bind); q != nil {
			t.Errorf("BeginQuery() = %v, want nil", q)
		}
	})

	t.Run("question-mark placeholder with sqlfmt.BindQuestion", func(t *testing.T) {
		ctx, d := newCtx()

		q := tracer.BeginQuery(ctx, "-- name: GetUser :one\nSELECT email FROM users WHERE id = ?", []any{7}, sqlfmt.BindQuestion)
		q.Finish(tracer.SQLData{}, nil)

		logs := sqlEntries(t, d)
		if len(logs) != 1 || logs[0].Label != "GetUser" {
			t.Fatalf("Logs() = %+v, want one GetUser entry", logs)
		}

		if data := logs[0].Data.(tracer.SQLData); data.SQL != "SELECT email FROM users WHERE id = 7" {
			t.Errorf("SQL = %q, want the ? placeholder bound", data.SQL)
		}
	})
}
