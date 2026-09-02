package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

// scanner is satisfied by both *sql.Row and *sql.Rows, so one scan function
// serves a single read and a listing.
type scanner interface {
	Scan(dest ...any) error
}

// affected reports how many rows a statement changed, turning the driver's
// two-value answer into one.
func affected(result sql.Result, operation string) (int64, error) {
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", operation, err)
	}
	return count, nil
}

// resolveMiss explains a compare-and-swap that changed no rows. The revision in
// the WHERE clause is the only thing that can fail once the key exists, so an
// existing key means another writer moved the record on: ErrConflict. A missing
// key is ErrNotFound. Nothing above this package can tell the two apart.
func (tx *Tx) resolveMiss(ctx context.Context, what, existsQuery string, args ...any) error {
	var present int
	if err := tx.ex.QueryRowContext(ctx, existsQuery, args...).Scan(&present); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if present == 0 {
		return fmt.Errorf("%w: %s", model.ErrNotFound, what)
	}
	return fmt.Errorf("%w: %s changed since it was read", model.ErrConflict, what)
}

// collect drains rows through scan, so every listing shares one error path.
func collect[T any](rows *sql.Rows, err error, operation string, scan func(scanner) (T, error)) ([]T, error) {
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	defer rows.Close()

	values := []T{}
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", operation, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return values, nil
}
