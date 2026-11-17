package tracer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jun-develop/junkit/sqlfmt"
)

// DBTX is the query-execution interface shared by pgx pool and transaction handles.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// maxTracedRows caps the number of rows captured per traced query.
const maxTracedRows = 20

// SQLData is the payload of a traced SQL query entry.
type SQLData struct {
	SQL       string `json:"sql,omitempty"`
	Operation string `json:"operation,omitempty"`

	// Result is the scanned row of a QueryRow.
	Result map[string]any `json:"result,omitempty"`

	// Rows are the scanned rows of a Query, up to maxTracedRows.
	Rows []map[string]any `json:"rows,omitempty"`

	// RowCount is the true total of rows a Query returned.
	RowCount     *int   `json:"row_count,omitempty"`
	RowsAffected *int64 `json:"rows_affected,omitempty"`
	Error        string `json:"error,omitempty"`

	// Location is set for failed queries: the application code that ran the query.
	Location string `json:"location,omitempty"`
	Function string `json:"function,omitempty"`

	// query is the SQL before binding args; it groups repeated queries for N+1 detection.
	query string
}

// PgxWrapper wraps a pgx pool or transaction handle to record queries on ctx's Debugger.
type PgxWrapper struct {
	inner DBTX
}

// NewPgxWrapper wraps inner, which may be a pool or a pgx.Tx.
//
// Usage:
//
//	postgres.Options{Decorate: func(inner postgres.DBTX) postgres.DBTX {
//	    return tracer.NewPgxWrapper(inner)
//	}}
func NewPgxWrapper(inner DBTX) *PgxWrapper {
	return &PgxWrapper{inner: inner}
}

// Exec implements DBTX; it records rows affected when ctx has a Debugger.
func (w *PgxWrapper) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	q := BeginQuery(ctx, sql, args, sqlfmt.Bind)
	if q == nil {
		return w.inner.Exec(ctx, sql, args...)
	}

	tag, err := w.inner.Exec(ctx, sql, args...)
	n := tag.RowsAffected()
	q.Finish(SQLData{RowsAffected: &n}, err)

	return tag, err
}

// Query implements DBTX; it records scanned rows when ctx has a Debugger.
func (w *PgxWrapper) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q := BeginQuery(ctx, sql, args, sqlfmt.Bind)
	if q == nil {
		return w.inner.Query(ctx, sql, args...)
	}

	rows, err := w.inner.Query(ctx, sql, args...)
	if err != nil {
		q.Finish(SQLData{}, err)
		return rows, err
	}

	return &tracedRows{Rows: rows, q: q}, nil
}

// QueryRow implements DBTX; it records the scanned row when ctx has a Debugger.
func (w *PgxWrapper) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	q := BeginQuery(ctx, sql, args, sqlfmt.Bind)
	if q == nil {
		return w.inner.QueryRow(ctx, sql, args...)
	}

	// Query instead of QueryRow so the column names are available at Scan time.
	rows, err := w.inner.Query(ctx, sql, args...)
	if err != nil {
		q.Finish(SQLData{}, err)
		return errRow{err}
	}

	return &tracedRow{rows: rows, q: q}
}

// Query is a traced query that has started but not yet been recorded. Wrappers for other drivers
// use BeginQuery and Finish the same way PgxWrapper does.
type Query struct {
	d     *Debugger
	start time.Time
	atMS  float64
	info  sqlfmt.Info

	// sql is the SQL with arguments bound, for display.
	sql string
}

// BeginQuery starts tracing a query, or returns nil when ctx has no Debugger. bind renders args
// into sql for display and must match the driver's placeholders (sqlfmt.Bind for $1,
// sqlfmt.BindQuestion for ?).
//
// Usage:
//
//	q := tracer.BeginQuery(ctx, sql, args, sqlfmt.BindQuestion)
//	if q == nil {
//	    return inner.ExecContext(ctx, sql, args...)
//	}
//	res, err := inner.ExecContext(ctx, sql, args...)
//	q.Finish(tracer.SQLData{}, err)
func BeginQuery(ctx context.Context, sql string, args []any, bind func(sql string, args []any) string) *Query {
	d := FromContext(ctx)
	if d == nil {
		return nil
	}

	info := sqlfmt.Parse(sql)

	return &Query{
		d:     d,
		start: time.Now(),
		atMS:  d.sinceStartMS(),
		info:  info,
		sql:   bind(info.SQL, args),
	}
}

// Finish records the query with its result payload and error.
func (q *Query) Finish(data SQLData, err error) {
	data.SQL = q.sql
	data.query = q.info.SQL
	data.Operation = q.info.Operation
	if err != nil {
		data.Error = err.Error()
		data.Location, data.Function = sqlfmt.Origin()
	}

	q.d.Append(LogEntry{Type: TypeSQL, Label: q.info.Name, AtMS: q.atMS, DurationMS: msSince(q.start), Data: data})
}

// errRow implements pgx.Row for a query that failed before any row was read.
type errRow struct{ err error }

// Scan implements pgx.Row.
func (r errRow) Scan(...any) error { return r.err }

// tracedRow implements pgx.Row; it records the scanned row.
type tracedRow struct {
	rows pgx.Rows
	q    *Query
}

// Scan implements pgx.Row.
func (r *tracedRow) Scan(dest ...any) error {
	defer r.rows.Close()

	if !r.rows.Next() {
		err := r.rows.Err()
		if err == nil {
			err = pgx.ErrNoRows
		}

		r.q.Finish(SQLData{}, err)

		return err
	}

	err := r.rows.Scan(dest...)
	if err == nil {
		err = r.rows.Err()
	}

	r.q.Finish(SQLData{Result: rowMap(r.rows.FieldDescriptions(), dest)}, err)

	return err
}

// tracedRows wraps pgx.Rows, collecting scanned rows and recording them on Close.
type tracedRows struct {
	pgx.Rows
	q      *Query
	rows   []map[string]any
	total  int
	closed bool
}

// Scan implements pgx.Rows; it captures up to maxTracedRows rows.
func (r *tracedRows) Scan(dest ...any) error {
	if err := r.Rows.Scan(dest...); err != nil {
		return err
	}

	r.total++
	if len(r.rows) < maxTracedRows {
		r.rows = append(r.rows, rowMap(r.Rows.FieldDescriptions(), dest))
	}

	return nil
}

// Close implements pgx.Rows; it records the query once even if called repeatedly.
func (r *tracedRows) Close() {
	r.Rows.Close()
	if r.closed {
		return
	}

	r.closed = true
	r.q.Finish(SQLData{Rows: r.rows, RowCount: &r.total}, r.Rows.Err())
}

// rowMap pairs column names with the scanned values in dest.
func rowMap(fds []pgconn.FieldDescription, dest []any) map[string]any {
	row := make(map[string]any, len(dest))
	for i, d := range dest {
		key := fmt.Sprintf("col_%d", i)
		if i < len(fds) && fds[i].Name != "" {
			key = fds[i].Name
		}

		row[key] = displayValue(d)
	}

	return row
}

// displayValue dereferences a Scan destination and makes raw bytes readable:
// JSON (e.g. a jsonb column) is kept as JSON, anything else becomes hex.
func displayValue(dest any) any {
	v := reflect.ValueOf(dest)
	if v.Kind() == reflect.Pointer && !v.IsNil() {
		dest = v.Elem().Interface()
	}

	if b, ok := dest.([]byte); ok {
		if json.Valid(b) {
			return json.RawMessage(b)
		}

		return hex.EncodeToString(b)
	}

	return dest
}
