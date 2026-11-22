package postgres_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: tx_test.go
// Purpose: Verify DB.Executor (pool vs. active transaction) and DB.WithTransaction
//          (begin, commit, rollback, panic, nesting), using a fake pool and transaction.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 9
//
// #  Test Case                                   Scenario                                  Expected Result
// 1  TestDB_ExecutorReturnsPoolOrOwnTransaction  Outside a transaction                     Returns the pool
// 2  TestDB_ExecutorReturnsPoolOrOwnTransaction  Inside a transaction begun on another DB  Returns its own pool, not the other DB's transaction
// 3  TestDB_WithTransactionCommitsOrRollsBack    fn succeeds                               Commits; no rollback
// 4  TestDB_WithTransactionCommitsOrRollsBack    fn returns an error                       Returns that error; rolls back; no commit
// 5  TestDB_WithTransactionCommitsOrRollsBack    fn panics                                 Rolls back, no commit, re-panics with the same value
// 6  TestDB_WithTransactionCommitsOrRollsBack    Begin fails                               Returns the Begin error; fn is not run
// 7  TestDB_WithTransactionCommitsOrRollsBack    Commit fails                              Returns an error wrapping the commit error
// 8  TestDB_WithTransactionCommitsOrRollsBack    Nested WithTransaction call               Joins the outer transaction; only the outer call commits
// 9  TestDB_WithTransactionCommitsOrRollsBack    Executor inside fn                        Returns the active transaction
//
// =============================================================================

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jun-develop/junkit/database/postgres"
)

// fakeTx mocks pgx.Tx for testing, overriding Commit and Rollback.
type fakeTx struct {
	pgx.Tx
	commitErr  error
	committed  bool
	rolledBack bool
}

// Commit records the commit and returns commitErr.
func (f *fakeTx) Commit(context.Context) error {
	f.committed = true
	return f.commitErr
}

// Rollback records the rollback.
func (f *fakeTx) Rollback(context.Context) error {
	f.rolledBack = true
	return nil
}

// fakePool mocks DBPool for testing, overriding Begin.
type fakePool struct {
	postgres.DBPool
	tx       *fakeTx
	beginErr error
}

// Begin returns tx, or beginErr when set.
func (f *fakePool) Begin(context.Context) (pgx.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}

	return f.tx, nil
}

func TestDB_ExecutorReturnsPoolOrOwnTransaction(t *testing.T) {
	t.Run("returns the pool outside a transaction", func(t *testing.T) {
		pool := &fakePool{}
		db := postgres.FromPool(pool)

		if got := db.Executor(context.Background()); got != pool {
			t.Fatalf("Executor() = %v, want the pool", got)
		}
	})

	t.Run("ignores a transaction begun on another DB", func(t *testing.T) {
		primary := postgres.FromPool(&fakePool{tx: &fakeTx{}})
		otherPool := &fakePool{}
		other := postgres.FromPool(otherPool)

		err := primary.WithTransaction(context.Background(), func(txCtx context.Context) error {
			if got := other.Executor(txCtx); got != otherPool {
				t.Fatalf("other.Executor(txCtx) = %v, want other's own pool, not primary's transaction", got)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction() error = %v, want nil", err)
		}
	})
}

func TestDB_WithTransactionCommitsOrRollsBack(t *testing.T) {
	t.Run("commits on success", func(t *testing.T) {
		tx := &fakeTx{}
		db := postgres.FromPool(&fakePool{tx: tx})

		err := db.WithTransaction(context.Background(), func(context.Context) error {
			return nil
		})

		if err != nil {
			t.Fatalf("WithTransaction() error = %v, want nil", err)
		}

		if !tx.committed {
			t.Fatal("transaction committed = false, want true")
		}

		if tx.rolledBack {
			t.Fatal("transaction rolled back = true, want false on success")
		}
	})

	t.Run("rolls back when fn returns an error", func(t *testing.T) {
		tx := &fakeTx{}
		db := postgres.FromPool(&fakePool{tx: tx})
		wantErr := errors.New("boom")

		err := db.WithTransaction(context.Background(), func(context.Context) error {
			return wantErr
		})

		if !errors.Is(err, wantErr) {
			t.Fatalf("WithTransaction() error = %v, want %v", err, wantErr)
		}

		if tx.committed {
			t.Fatal("transaction committed = true, want false when fn returns an error")
		}

		if !tx.rolledBack {
			t.Fatal("transaction rolled back = false, want true when fn returns an error")
		}
	})

	t.Run("rolls back and re-panics when fn panics", func(t *testing.T) {
		tx := &fakeTx{}
		db := postgres.FromPool(&fakePool{tx: tx})

		defer func() {
			r := recover()
			if r != "fn panicked" {
				t.Fatalf("recover() = %v, want %q", r, "fn panicked")
			}

			if !tx.rolledBack {
				t.Fatal("transaction rolled back = false, want true after a panic")
			}

			if tx.committed {
				t.Fatal("transaction committed = true, want false after a panic")
			}
		}()

		_ = db.WithTransaction(context.Background(), func(context.Context) error {
			panic("fn panicked")
		})
	})

	t.Run("propagates a begin error without touching the transaction", func(t *testing.T) {
		beginErr := errors.New("connection refused")
		db := postgres.FromPool(&fakePool{beginErr: beginErr})

		err := db.WithTransaction(context.Background(), func(context.Context) error {
			t.Fatal("fn should not run when Begin fails")
			return nil
		})

		if !errors.Is(err, beginErr) {
			t.Fatalf("WithTransaction() error = %v, want it to wrap %v", err, beginErr)
		}
	})

	t.Run("wraps a commit error", func(t *testing.T) {
		commitErr := errors.New("commit failed")
		tx := &fakeTx{commitErr: commitErr}
		db := postgres.FromPool(&fakePool{tx: tx})

		err := db.WithTransaction(context.Background(), func(context.Context) error {
			return nil
		})

		if !errors.Is(err, commitErr) {
			t.Fatalf("WithTransaction() error = %v, want it to wrap %v", err, commitErr)
		}
	})

	t.Run("a nested call joins the outer transaction", func(t *testing.T) {
		tx := &fakeTx{}
		pool := &fakePool{tx: tx}
		db := postgres.FromPool(pool)

		err := db.WithTransaction(context.Background(), func(outerCtx context.Context) error {
			pool.beginErr = errors.New("nested Begin must not be called")

			return db.WithTransaction(outerCtx, func(innerCtx context.Context) error {
				if got := db.Executor(innerCtx); got != tx {
					t.Fatal("nested fn does not run in the outer transaction")
				}

				if tx.committed {
					t.Fatal("the nested call committed the outer transaction")
				}

				return nil
			})
		})
		if err != nil {
			t.Fatalf("WithTransaction() error = %v, want nil", err)
		}

		if !tx.committed {
			t.Fatal("outer transaction committed = false, want true")
		}
	})

	t.Run("fn's context carries the active transaction", func(t *testing.T) {
		tx := &fakeTx{}
		db := postgres.FromPool(&fakePool{tx: tx})

		err := db.WithTransaction(context.Background(), func(ctx context.Context) error {
			if got := db.Executor(ctx); got != tx {
				t.Fatal("fn's context does not carry the transaction WithTransaction began")
			}

			return nil
		})
		if err != nil {
			t.Fatalf("WithTransaction() error = %v, want nil", err)
		}
	})
}
