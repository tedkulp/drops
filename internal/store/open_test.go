package store_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/store"
	_ "modernc.org/sqlite"
)

// inspect opens a second, independent connection to a store file. Every
// assertion about persisted bytes reads through it rather than through the
// package under test, so a broken writer cannot agree with a broken reader.
func inspect(t *testing.T, path string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open inspection connection: %v", err)
	}
	t.Cleanup(func() { raw.Close() })
	return raw
}

func scalar[T any](t *testing.T, db *sql.DB, query string, args ...any) T {
	t.Helper()
	var value T
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}

func TestOpenCreatesTheV7Schema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drops.db")

	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open fresh store: %v", err)
	}
	defer opened.Close()

	raw := inspect(t, path)
	if version := scalar[int](t, raw, "PRAGMA user_version"); version != 7 {
		t.Errorf("user_version = %d, want 7", version)
	}
	if rows := scalar[int](t, raw, "SELECT count(*) FROM store_state WHERE id = 1"); rows != 1 {
		t.Errorf("store_state singleton rows = %d, want 1", rows)
	}
	for _, table := range []string{"issues_fts", "comments_fts", "memories_fts"} {
		got := scalar[int](t, raw,
			"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table)
		if got != 1 {
			t.Errorf("FTS index %s present = %d, want 1", table, got)
		}
	}
}

func TestOpenReusesAnExistingStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drops.db")

	first, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open fresh store: %v", err)
	}
	if _, err := first.DB().ExecContext(t.Context(),
		"UPDATE store_state SET write_seq = 41 WHERE id = 1"); err != nil {
		t.Fatalf("write through first handle: %v", err)
	}
	first.Close()

	second, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer second.Close()

	if seq := scalar[int](t, inspect(t, path), "SELECT write_seq FROM store_state"); seq != 41 {
		t.Errorf("write_seq after reopen = %d, want 41 (reopen must not recreate the schema)", seq)
	}
}

// TestOpenRefusesADatabaseWithAnotherSchemaVersion is the surviving half of the
// v6 story. The one-time conversion is gone, so a v6 database that turns up now
// is a restore-from-backup problem and the only right answer is a refusal.
//
// It asserts the MESSAGE, not just the sentinel, and that is the point. A v6
// store carries tables, so the populated-but-unversioned guard below returns
// ErrUnsupportedSchema for it too: an errors.Is assertion alone cannot tell the
// version guard from the guard that catches it a step later, and stays green
// with the version guard removed.
func TestOpenRefusesADatabaseWithAnotherSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	seedRaw(t, path, "CREATE TABLE issues (id TEXT PRIMARY KEY); PRAGMA user_version = 6;")

	_, err := store.Open(t.Context(), path)
	if !errors.Is(err, store.ErrUnsupportedSchema) {
		t.Fatalf("open v6 store: err = %v, want ErrUnsupportedSchema", err)
	}
	if want := "reports schema version 6, want 7"; !strings.Contains(err.Error(), want) {
		t.Errorf("open v6 store: err = %q, want it to contain %q", err, want)
	}
}

func TestOpenRefusesAPopulatedDatabaseWithNoSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")
	seedRaw(t, path, "CREATE TABLE something_else (id TEXT PRIMARY KEY);")

	_, err := store.Open(t.Context(), path)
	if !errors.Is(err, store.ErrUnsupportedSchema) {
		t.Fatalf("open foreign database: err = %v, want ErrUnsupportedSchema", err)
	}
}

func TestOpenTightensPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "store")
	path := filepath.Join(dir, "drops.db")

	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open fresh store: %v", err)
	}
	defer opened.Close()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat store directory: %v", err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Errorf("store directory mode = %#o, want 0700", mode)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0o600 {
		t.Errorf("database mode = %#o, want 0600", mode)
	}
}

// seedRaw writes a database this package did not create, so the refusal paths
// are exercised against a real file rather than a stubbed version number.
func seedRaw(t *testing.T, path, ddl string) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create seed database: %v", err)
	}
	defer raw.Close()
	if _, err := raw.Exec(ddl); err != nil {
		t.Fatalf("seed database: %v", err)
	}
}
