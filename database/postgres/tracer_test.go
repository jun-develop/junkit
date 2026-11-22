package postgres_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: tracer_test.go
// Purpose: Verify the tracer contract: every query run through DB.Executor reaches the
//          tracer.Debugger, inside and outside a transaction.
// Type: Unit (fake pool and transaction; queryTx embeds fakeTx from tx_test.go)
//
// Test functions: 1
// Total scenarios: 1
//
// #  Test Case                      Scenario                                                Expected Result
// 1  TestExecutor_TracesEveryQuery  Exec, Query, QueryRow outside and inside a transaction  All 6 queries traced in order with bound SQL; transaction committed
//
// =============================================================================

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jun-develop/junkit/database/postgres"
	"github.com/jun-develop/junkit/tracer"
)

// emptyRows is a pgx.Rows with no rows.
type emptyRows struct{ pgx.Rows }

// Next reports that there are no rows.
func (emptyRows) Next() bool { return false }

// Err reports no error.
func (emptyRows) Err() error { return nil }

// Close does nothing.
func (emptyRows) Close() {}

// FieldDescriptions returns no columns.
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

// noRow is a pgx.Row for a query that matched nothing.
type noRow struct{}

// Scan reports pgx.ErrNoRows.
func (noRow) Scan(...any) error { return pgx.ErrNoRows }

// queryTx is a fakeTx that also answers queries, so traced statements can run inside a transaction.
type queryTx struct{ fakeTx }

// Exec reports one updated row.
func (*queryTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// Query returns no rows.
func (*queryTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return emptyRows{}, nil }

// QueryRow returns a row that matched nothing.
func (*queryTx) QueryRow(context.Context, string, ...any) pgx.Row { return noRow{} }

// queryPool is a DBPool that answers queries and begins a queryTx.
type queryPool struct {
	postgres.DBPool
	tx *queryTx
}

// Exec reports one updated row.
func (*queryPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// Query returns no rows.
func (*queryPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return emptyRows{}, nil }

// QueryRow returns a row that matched nothing.
func (*queryPool) QueryRow(context.Context, string, ...any) pgx.Row { return noRow{} }

// Begin returns tx.
func (p *queryPool) Begin(context.Context) (pgx.Tx, error) { return p.tx, nil }

// TestExecutor_TracesEveryQuery is the tracer contract every engine package must pass: every query
// run through Executor, sqlc-generated or raw, inside or outside a transaction, reaches the Debugger.
func TestExecutor_TracesEveryQuery(t *testing.T) {
	tx := &queryTx{}
	db := postgres.NewDBWithDecorateForTest(&queryPool{tx: tx}, func(inner postgres.DBTX) postgres.DBTX {
		return tracer.NewPgxWrapper(inner)
	})

	d := tracer.New()
	ctx := tracer.WithContext(context.Background(), d)

	run := func(ctx context.Context) {
		if _, err := db.Executor(ctx).Exec(ctx, "UPDATE users SET seen = $1", true); err != nil {
			t.Fatalf("Exec() error = %v", err)
		}

		rows, err := db.Executor(ctx).Query(ctx, "SELECT id FROM users")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		rows.Close()

		var id int
		if err := db.Executor(ctx).QueryRow(ctx, "SELECT id FROM users WHERE id = $1", 7).Scan(&id); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("QueryRow().Scan() error = %v, want pgx.ErrNoRows", err)
		}
	}

	run(ctx)

	if err := db.WithTransaction(ctx, func(txCtx context.Context) error {
		run(txCtx)
		return nil
	}); err != nil {
		t.Fatalf("WithTransaction() error = %v", err)
	}

	if !tx.committed {
		t.Fatal("transaction committed = false, want true")
	}

	want := []string{
		"UPDATE users SET seen = true",
		"SELECT id FROM users",
		"SELECT id FROM users WHERE id = 7",
	}
	want = append(want, want...)

	logs := d.Logs()
	if len(logs) != len(want) {
		t.Fatalf("got %d traced queries, want %d (3 outside and 3 inside the transaction)", len(logs), len(want))
	}

	for i, e := range logs {
		if got := e.Data.(tracer.SQLData).SQL; got != want[i] {
			t.Errorf("query %d SQL = %q, want %q", i, got, want[i])
		}
	}
}
