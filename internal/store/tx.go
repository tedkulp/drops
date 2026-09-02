package store

import (
	"context"
	"database/sql"
	"fmt"
)

// execer is the shared surface of *sql.DB and *sql.Tx. It is the only reason
// this package declares an interface at all: there is one SQLite adapter, so a
// Store interface would be a seam with nothing on the other side of it.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// reader carries every read. It is embedded in both Store and Tx so a query
// reads the same rows inside and outside a transaction, from one implementation.
type reader struct {
	ex execer
}

// Tx is one open transaction. Writes exist only here: a replicated write and
// its write-counter bump must land together, so there is no autocommit path
// that could commit one without the other.
type Tx struct {
	reader

	tx *sql.Tx
}

// WithTx runs fn inside one transaction, committing when it returns nil and
// rolling back on any error or panic. Domain transaction boundaries are the
// caller's: this package composes no rule of its own across two records.
func (store *Store) WithTx(ctx context.Context, fn func(context.Context, *Tx) error) (err error) {
	sqlTx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	tx := &Tx{reader: reader{ex: sqlTx}, tx: sqlTx}

	defer func() {
		if recovered := recover(); recovered != nil {
			sqlTx.Rollback()
			panic(recovered)
		}
		if err != nil {
			sqlTx.Rollback()
		}
	}()

	if err = fn(ctx, tx); err != nil {
		return err
	}
	if err = sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
