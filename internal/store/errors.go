package store

import (
	"errors"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
	sqlite "modernc.org/sqlite"
)

// SQLite extended result codes, measured against modernc.org/sqlite v1.57.0.
// The driver surfaces the extended code, not the primary one, so a UNIQUE
// violation on a PRIMARY KEY (1555) is distinguishable from one on any other
// unique index (2067).
const (
	constraintCheck      = 275
	constraintForeignKey = 787
	constraintNotNull    = 1299
	constraintPrimaryKey = 1555
	constraintUnique     = 2067
	constraintTrigger    = 1811
)

// classify maps a driver error onto the stable error classes the exit-code
// contract is built from. This is the only layer that can see the difference, so
// it makes the call here rather than leaving callers to match on message text.
//
//   - A foreign key violation means a row this write references is absent, which
//     is ErrNotFound however the caller phrased the write.
//   - A primary key or unique violation means the key is already taken:
//     ErrConflict.
//   - A CHECK, NOT NULL or trigger violation means the values were never
//     writable: ErrInvalid.
func classify(operation string, err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch sqliteErr.Code() {
	case constraintForeignKey:
		return fmt.Errorf("%s: %w: %v", operation, model.ErrNotFound, err)
	case constraintPrimaryKey, constraintUnique:
		return fmt.Errorf("%s: %w: %v", operation, model.ErrConflict, err)
	case constraintCheck, constraintNotNull, constraintTrigger:
		return fmt.Errorf("%s: %w: %v", operation, model.ErrInvalid, err)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
