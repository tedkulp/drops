package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// legacyMirrorArtifacts are the files the v6 build kept beside its store. The
// v7 transport writes into the same directory (mirror.jsonl, replica.json,
// .git/), and its .git history commits a different file set entirely, so an
// inherited repository would leave the eventual shared remote reconciling two
// unrelated histories, one of them in a format nothing can now read.
//
// They are moved into the backup rather than deleted: they are the only other
// copy of the pre-cutover rows.
var legacyMirrorArtifacts = []string{
	"issues.jsonl", "memories.jsonl", "projects.jsonl",
	".git", ".gitattributes", ".gitignore", ".nohooks",
}

// Backup writes a consistent copy of the v6 store into dir and proves it, then
// moves the legacy mirror aside. It returns the backup's path.
//
// The copy is made with VACUUM INTO rather than by copying bytes: the source is
// a WAL database, so its file alone is not necessarily the whole store, and a
// backup that silently omits the log is exactly the kind of restore that is
// discovered to be useless at the moment it is needed.
func Backup(ctx context.Context, storePath, dir string, want Census) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	backup := filepath.Join(dir, filepath.Base(storePath))
	if _, err := os.Stat(backup); err == nil {
		return "", fmt.Errorf("backup %s already exists; refusing to overwrite it", backup)
	}
	if err := copyStore(ctx, storePath, backup); err != nil {
		return "", err
	}
	if err := proveBackup(ctx, backup, want); err != nil {
		return "", err
	}
	if err := moveLegacyMirror(filepath.Dir(storePath), dir); err != nil {
		return "", err
	}
	return backup, nil
}

// copyStore makes a self-contained copy of a SQLite database.
func copyStore(ctx context.Context, from, to string) error {
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(from)+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return fmt.Errorf("open %s to copy it: %w", from, err)
	}
	defer db.Close()
	// VACUUM INTO takes a read lock for the duration and writes one consistent
	// file, WAL content included.
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, to); err != nil {
		return fmt.Errorf("copy %s to %s: %w", from, to, err)
	}
	return nil
}

// proveBackup reads the copy back and requires it to hold what the source held.
//
// AGENTS.md's standing lesson is that a restore is proved rather than trusted.
// A checksum would only say the bytes arrived; this opens the copy as a
// database, runs SQLite's own integrity check over it, and re-counts every row
// the conversion is about to depend on.
func proveBackup(ctx context.Context, backup string, want Census) error {
	db, err := sql.Open("sqlite", "file:"+url.PathEscape(backup)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open the backup to prove it: %w", err)
	}
	var result string
	err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result)
	db.Close()
	if err != nil {
		return fmt.Errorf("prove the backup: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("the backup at %s fails integrity_check: %s", backup, result)
	}

	got, err := TakeCensus(ctx, backup)
	if err != nil {
		return fmt.Errorf("prove the backup: %w", err)
	}
	if got.Counts != want.Counts {
		return fmt.Errorf("the backup at %s holds %+v, the store holds %+v",
			backup, got.Counts, want.Counts)
	}
	return nil
}

// moveLegacyMirror relocates the v6 transport's files out of the store
// directory. An artifact that is not there is not an error: the second machine
// need not have synced.
func moveLegacyMirror(storeDir, backupDir string) error {
	target := filepath.Join(backupDir, "legacy-mirror")
	moved := false
	for _, name := range legacyMirrorArtifacts {
		from := filepath.Join(storeDir, name)
		if _, err := os.Lstat(from); err != nil {
			continue
		}
		if !moved {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			moved = true
		}
		if err := os.Rename(from, filepath.Join(target, name)); err != nil {
			return fmt.Errorf("move %s aside: %w", name, err)
		}
	}
	return nil
}
