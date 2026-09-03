package sync

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileAtomicForcesTheModeOverAPreExistingTemp is the guard the comment
// in file.go calls load-bearing: O_CREATE|O_TRUNC keeps an existing temp's own
// mode, and that mode rides the rename onto the mirror — a world-readable copy
// of every issue and memory in the store.
func TestWriteFileAtomicForcesTheModeOverAPreExistingTemp(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, tempName(mirrorFileName))
	if err := os.WriteFile(tmp, []byte("stale"), 0o666); err != nil {
		t.Fatalf("seed temp: %v", err)
	}
	if err := os.Chmod(tmp, 0o666); err != nil {
		t.Fatalf("chmod temp: %v", err)
	}

	if err := writeFileAtomic(dir, mirrorFileName, []byte("body\n")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dir, mirrorFileName))
	if err != nil {
		t.Fatalf("stat mirror: %v", err)
	}
	// The literal, not fileMode: comparing the file against the constant that
	// wrote it can never disagree with the code.
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mirror mode = %04o, want 0600", perm)
	}
}

// TestWriteFileAtomicReplacesThroughATemp: the bytes land under a dotted name
// first, so a crash mid-write can never leave a truncated file under the name
// the next sync would stage and push.
func TestWriteFileAtomicReplacesThroughATemp(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, mirrorFileName)
	if err := os.WriteFile(final, []byte("old\n"), fileMode); err != nil {
		t.Fatalf("seed mirror: %v", err)
	}

	if name := tempName(mirrorFileName); name == mirrorFileName || name[0] != '.' {
		t.Fatalf("tempName(%q) = %q, want a distinct dotted name", mirrorFileName, name)
	}
	if err := writeFileAtomic(dir, mirrorFileName, []byte("new\n")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(final)
	if err != nil || string(got) != "new\n" {
		t.Fatalf("mirror = (%q, %v), want the replacement", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, tempName(mirrorFileName))); !os.IsNotExist(err) {
		t.Fatalf("stat temp = %v, want it gone after the rename", err)
	}
}

// TestWriteFileAtomicLeavesNothingWhenItCannotWrite: a failed write reports and
// cleans up rather than leaving a stageable artifact behind.
func TestWriteFileAtomicLeavesNothingWhenItCannotWrite(t *testing.T) {
	dir := t.TempDir()
	// A directory under the temp's name cannot be opened for writing, so the
	// very first step fails.
	if err := os.Mkdir(filepath.Join(dir, tempName(mirrorFileName)), 0o700); err != nil {
		t.Fatalf("seed blocking directory: %v", err)
	}

	if err := writeFileAtomic(dir, mirrorFileName, []byte("body\n")); err == nil {
		t.Fatal("writeFileAtomic over an unwritable temp = nil error, want a failure")
	}
	if _, err := os.Stat(filepath.Join(dir, mirrorFileName)); !os.IsNotExist(err) {
		t.Fatalf("stat mirror = %v, want no file under the final name", err)
	}
}

func TestFsyncDirReportsAMissingDirectory(t *testing.T) {
	if err := fsyncDir(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("fsyncDir on a missing directory = nil error, want a failure")
	}
}
