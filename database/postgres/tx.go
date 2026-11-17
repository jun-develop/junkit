package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// txContextKey is the context key under which d's active transaction is stored. It carries the
// owning DB so that, with several databases, one DB's Executor never picks up another's transaction.
type txContextKey struct {
	db *DB
}

// withTx returns a copy of ctx carrying tx as d's active transaction.
func (d *DB) withTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txContextKey{db: d}, tx)
}

// getTx retrieves d's transaction attached to ctx by withTx, if any.
func (d *DB) getTx(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txContextKey{db: d}).(pgx.Tx)
	return tx, ok
}

// WithTransaction runs fn inside a transaction, committing on success and rolling back on error
// or panic (the panic is re-raised). fn's context carries the transaction, so Executor uses it.
// A nested call on a ctx that already carries d's transaction joins it: fn runs in the outer
// transaction, which alone commits or rolls back.
//
// Usage:
//
//	err := db.WithTransaction(ctx, func(txCtx context.Context) error {
//	    if err := repo.Create(txCtx, user); err != nil {
//	        return err
//	    }
//	    return repo.CreateAuditLog(txCtx, entry)
//	})
func (d *DB) WithTransaction(ctx context.Context, fn func(context.Context) error) (err error) {
	if _, ok := d.getTx(ctx); ok {
		return fn(ctx)
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		} else if err != nil {
			_ = tx.Rollback(ctx)
		} else if commitErr := tx.Commit(ctx); commitErr != nil {
			err = fmt.Errorf("postgres: commit transaction: %w", commitErr)
		}
	}()

	err = fn(d.withTx(ctx, tx))

	return err
}
