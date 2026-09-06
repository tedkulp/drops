package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/tedkulp/drops/internal/model"
	_ "modernc.org/sqlite"
)

// SchemaVersion is the only schema this build reads or writes. A fresh store is
// created at this version directly; migrations 1 through 6 are not carried
// forward, and no older shape is converted — both machines have converted, so
// the one-time v6 conversion is gone and a v6 store is refused like any other
// foreign database.
const SchemaVersion = 7

//go:embed schema/v7.sql
var schemaDDL string

// ErrUnsupportedSchema reports a database that is neither empty nor a store this
// build understands. It is never repaired in place.
var ErrUnsupportedSchema = errors.New("unsupported schema")

// Store is the SQLite adapter. It owns connection setup, schema, statement
// construction, and the monotonic write counter, and owns no transaction
// boundary above a single record: callers compose those with WithTx.
type Store struct {
	reader

	db   *sql.DB
	path string
}

// Path reports the file this Store was opened from. Sync and doctor observe the
// database alongside its sidecar and mirror, so the path is part of the store's
// identity rather than caller bookkeeping.
func (store *Store) Path() string { return store.path }

// DB exposes the pool for the integrity operations that must run outside a
// transaction. Nothing above this package builds SQL.
func (store *Store) DB() *sql.DB { return store.db }

func (store *Store) Close() error { return store.db.Close() }

// DefaultPath returns $DROPS_DB when set, else ~/.drops/drops.db.
func DefaultPath() (string, error) {
	if configured := os.Getenv("DROPS_DB"); configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".drops", "drops.db"), nil
}

// pragmaQuery is consumed by modernc.org/sqlite itself and re-applied to every
// pooled connection, which is what WAL and the busy timeout need.
//
// _txlock=immediate takes the write lock at BEGIN rather than on the first
// write inside it. Go's deferred BEGIN lets N transactions open together and
// then race to upgrade, and that upgrade collision returns SQLITE_BUSY
// immediately instead of waiting out busy_timeout, which covers only waiting
// for an already-held lock.
const pragmaQuery = "_pragma=journal_mode(WAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(ON)" +
	"&_txlock=immediate"

// dsn builds the connection string for a filesystem path.
//
// The path is a URI component and must be escaped, not concatenated. Three
// classes used to corrupt the DSN silently: '?' ends the path early because
// modernc splits at the first literal '?', so the pragmas are lost; '#' starts a
// fragment, so two paths differing only after it collide onto one database; and
// '%' is decoded, so "a%41b.db" opens "aAb.db". EscapedPath covers all three
// while leaving '/' a separator. The "file:" prefix is concatenated rather than
// built with url.URL.String, which would emit an authority and turn a relative
// path into a hostname.
func dsn(path string) string {
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + pragmaQuery
}

// Open connects to path, creating the store directory and a fresh v7 schema when
// they do not exist, and refusing any other populated shape.
func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create store directory: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("chmod store directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	// Tighten the mode BEFORE anything is written. SQLite gives the -wal and
	// -shm sidecars the main file's permissions at first write, so a chmod that
	// runs afterwards leaves issue and memory bodies world-readable in -wal.
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("chmod database: %w", err)
	}

	store := &Store{db: db, path: path}
	store.reader = reader{ex: db}
	if err := store.bootstrap(ctx); err != nil {
		db.Close()
		return nil, err
	}

	// Inheritance is observed SQLite behaviour rather than a documented
	// contract, and a security property should not rest on it.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(path+suffix, 0o600); err != nil && !os.IsNotExist(err) {
			db.Close()
			return nil, fmt.Errorf("chmod %s: %w", path+suffix, err)
		}
	}
	return store, nil
}

// bootstrap creates the schema in an empty database and otherwise refuses
// anything that is not already v7. Nothing converts an older store: a v6
// database that turns up now is a restore-from-backup problem, and answering it
// with a silent open would be worse than refusing it.
func (store *Store) bootstrap(ctx context.Context) error {
	version, err := store.userVersion(ctx)
	if err != nil {
		return err
	}
	switch {
	case version == SchemaVersion:
		return nil
	case version != 0:
		return fmt.Errorf("%w: database at %s reports schema version %d, want %d",
			ErrUnsupportedSchema, store.path, version, SchemaVersion)
	}

	populated, err := store.hasUserTables(ctx)
	if err != nil {
		return err
	}
	if populated {
		return fmt.Errorf("%w: database at %s carries tables but no schema version",
			ErrUnsupportedSchema, store.path)
	}
	return store.createSchema(ctx)
}

func (store *Store) userVersion(ctx context.Context) (int, error) {
	var version int
	if err := store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func (store *Store) hasUserTables(ctx context.Context) (bool, error) {
	var count int
	const query = `SELECT count(*) FROM sqlite_master
	               WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'`
	if err := store.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect schema: %w", err)
	}
	return count > 0, nil
}

// createSchema applies the whole v7 DDL in one transaction, so a failure part
// way through leaves an empty file rather than a partial store.
func (store *Store) createSchema(ctx context.Context) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema creation: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema creation: %w", err)
	}
	return nil
}

func wrapNotFound(what string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", model.ErrNotFound, what)
	}
	return err
}
