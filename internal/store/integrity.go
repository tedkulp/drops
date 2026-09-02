package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// FTSIndexes names the three external-content FTS5 indexes, in the order doctor
// reports them. There are three, not two: Comments are searched as well as
// Issues and Memories.
func FTSIndexes() []string {
	return []string{"issues_fts", "comments_fts", "memories_fts"}
}

// ForeignKeyViolation is one row PRAGMA foreign_key_check returned.
type ForeignKeyViolation struct {
	Table  string
	RowID  int64
	Parent string
	FKID   int64
}

func (violation ForeignKeyViolation) String() string {
	return fmt.Sprintf("%s row %d references missing %s (foreign key %d)",
		violation.Table, violation.RowID, violation.Parent, violation.FKID)
}

// QuickCheck runs PRAGMA quick_check and returns every line that is not "ok". A
// healthy database returns no findings, not one line saying it is fine.
func (store *Store) QuickCheck(ctx context.Context) ([]string, error) {
	rows, err := store.db.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return nil, fmt.Errorf("quick check: %w", err)
	}
	defer rows.Close()

	findings := []string{}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, fmt.Errorf("quick check: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(line), "ok") {
			findings = append(findings, line)
		}
	}
	return findings, rows.Err()
}

// ForeignKeyCheck returns every violation PRAGMA foreign_key_check found. The
// pragma reports violations as rows and raises no error, so an implementation
// that only inspects Exec's error learns nothing.
func (store *Store) ForeignKeyCheck(ctx context.Context) ([]ForeignKeyViolation, error) {
	rows, err := store.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, fmt.Errorf("foreign key check: %w", err)
	}
	defer rows.Close()

	violations := []ForeignKeyViolation{}
	for rows.Next() {
		var violation ForeignKeyViolation
		if err := rows.Scan(&violation.Table, &violation.RowID, &violation.Parent, &violation.FKID); err != nil {
			return nil, fmt.Errorf("foreign key check: %w", err)
		}
		violations = append(violations, violation)
	}
	return violations, rows.Err()
}

// CheckFTS runs the rank-1 integrity check on one index and reports what it
// found, or nil when the index agrees with its content table.
//
// The rank argument is load-bearing. Without it the check only verifies that the
// index is internally well formed, which stays true while the index has drifted
// from the content it claims to describe — in both directions, a missing entry
// and a stale one. Only rank 1 compares the index against the content table.
func (store *Store) CheckFTS(ctx context.Context, index string) error {
	if err := knownIndex(index); err != nil {
		return err
	}
	statement := fmt.Sprintf(
		`INSERT INTO %s(%s, rank) VALUES ('integrity-check', 1)`, index, index)
	if _, err := store.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("FTS integrity check on %s: %w", index, err)
	}
	return nil
}

// RebuildFTS rebuilds one index from its content table. It is the only repair
// this package performs, and only a caller that has seen CheckFTS fail should
// ask for it: rebuilding a healthy index is a pointless write.
func (store *Store) RebuildFTS(ctx context.Context, index string) error {
	if err := knownIndex(index); err != nil {
		return err
	}
	statement := fmt.Sprintf(`INSERT INTO %s(%s) VALUES ('rebuild')`, index, index)
	if _, err := store.db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("rebuild %s: %w", index, err)
	}
	return nil
}

// knownIndex refuses any name that is not one of the three. An index name is an
// SQL identifier and cannot be bound as a parameter, so the only safe way to
// interpolate one is to allow nothing else.
func knownIndex(index string) error {
	if !slices.Contains(FTSIndexes(), index) {
		return fmt.Errorf("unknown FTS index %q", index)
	}
	return nil
}
