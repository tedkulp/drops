package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// source is deliberately awkward: no trailing newline, a tab, and a repeated
// token, so a round trip that "tidies" anything is visible.
const source = "package p\n\nfunc f(a, b int) bool {\n\treturn a < b\n}\n\nfunc g(a, b int) bool {\n\treturn a > b\n}"

func writeSource(t *testing.T) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	file = filepath.Join(dir, "p.go")
	if err := os.WriteFile(file, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, file
}

func read(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyMutationReplacesTheSingleOccurrence(t *testing.T) {
	_, file := writeSource(t)
	if err := applyMutation(file, "return a < b", "return a <= b"); err != nil {
		t.Fatalf("applyMutation: %v", err)
	}
	want := "package p\n\nfunc f(a, b int) bool {\n\treturn a <= b\n}\n\nfunc g(a, b int) bool {\n\treturn a > b\n}"
	if got := read(t, file); got != want {
		t.Errorf("after mutation\n got %q\nwant %q", got, want)
	}
}

// A mutation that matches nothing has silently proved nothing, and one that
// matches twice has changed a branch nobody named. Both must refuse, and both
// must leave the file exactly as they found it.
func TestApplyMutationRefusesAnythingButOneOccurrence(t *testing.T) {
	tests := []struct {
		name  string
		old   string
		count int
	}{
		{"absent", "return a >= b", 0},
		{"repeated", "func ", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, file := writeSource(t)
			err := applyMutation(file, tc.old, "MUTATED")
			var occ *occurrenceError
			if !errors.As(err, &occ) {
				t.Fatalf("want an occurrenceError, got %v", err)
			}
			if occ.Count != tc.count {
				t.Errorf("Count = %d, want %d", occ.Count, tc.count)
			}
			if got := read(t, file); got != source {
				t.Errorf("file was modified by a refused mutation:\n%q", got)
			}
		})
	}
}

func TestRestoreReturnsTheExactBytes(t *testing.T) {
	dir, file := writeSource(t)
	snap, err := takeSnapshot(filepath.Join(dir, "snaps"), file)
	if err != nil {
		t.Fatalf("takeSnapshot: %v", err)
	}
	if err := applyMutation(file, "return a < b", "return true"); err != nil {
		t.Fatal(err)
	}
	if read(t, file) == source {
		t.Fatal("the mutation did not reach the file, so the restore proves nothing")
	}
	if err := snap.restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := read(t, file); got != source {
		t.Errorf("after restore\n got %q\nwant %q", got, source)
	}
}

// The restore is only worth anything if it is *verified*. A snapshot whose
// recorded checksum does not describe its own bytes must be an error, not a
// quiet write of whatever the copy happens to hold.
func TestRestoreVerifiesTheChecksum(t *testing.T) {
	dir, file := writeSource(t)
	snap, err := takeSnapshot(filepath.Join(dir, "snaps"), file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap.Path, []byte("something else entirely"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := snap.restore(); err == nil {
		t.Fatal("want an error when the restored bytes do not match the recorded checksum, got nil")
	}
}

func TestSnapshotPreservesFileMode(t *testing.T) {
	dir, file := writeSource(t)
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := takeSnapshot(filepath.Join(dir, "snaps"), file)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMutation(file, "return a < b", "return true"); err != nil {
		t.Fatal(err)
	}
	if err := snap.restore(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %v, want 0600", got)
	}
}
