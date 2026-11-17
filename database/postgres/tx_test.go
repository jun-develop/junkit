package postgres_test

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

func TestDB_ExecutorPicksPoolOrTx(t *testing.T) {
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
			t.Fatalf("unexpected error: %v", err)
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
			t.Fatal("expected the transaction to be committed")
		}

		if tx.rolledBack {
			t.Fatal("did not expect a rollback on success")
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
			t.Fatal("did not expect a commit when fn returned an error")
		}

		if !tx.rolledBack {
			t.Fatal("expected a rollback when fn returned an error")
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
				t.Fatal("expected a rollback after a panic")
			}

			if tx.committed {
				t.Fatal("did not expect a commit after a panic")
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
			t.Fatal("expected the outer transaction to be committed")
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
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
