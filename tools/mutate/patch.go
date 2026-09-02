package main

import (
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// occurrenceError reports a mutation whose `old` text did not appear in the
// file exactly once.
//
// Uniqueness is the safety property of the whole harness. Zero occurrences
// means the control has drifted from the code and the run would prove nothing;
// more than one means the edit lands somewhere nobody named, so a red says
// nothing about the branch the control claims. Both refuse before the file is
// touched, so a stale catalogue can never leave a mutation in the tree.
type occurrenceError struct {
	File  string
	Old   string
	Count int
}

func (e *occurrenceError) Error() string {
	if e.Count == 0 {
		return fmt.Sprintf("%s: no occurrence of %q — the control has drifted from the code", e.File, e.Old)
	}
	return fmt.Sprintf("%s: %d occurrences of %q — a mutation must name exactly one branch", e.File, e.Count, e.Old)
}

// applyMutation rewrites file, replacing the one occurrence of old with new.
func applyMutation(file, old, new string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	body := string(b)
	if n := strings.Count(body, old); n != 1 {
		return &occurrenceError{File: file, Old: old, Count: n}
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	return os.WriteFile(file, []byte(strings.Replace(body, old, new, 1)), info.Mode().Perm())
}

// snapshot is a harness-owned copy of a file's pre-mutation bytes.
//
// Harness-owned is the point, and AGENTS.md says why: a restore command cannot
// distinguish a mutation from uncommitted work, so `git checkout` would throw
// away whatever the author had in progress. The copy lives outside the tree and
// is described by its own checksum, so the restore can be *proved* rather than
// assumed.
type snapshot struct {
	File string      `json:"file"`     // the file that was mutated
	Path string      `json:"snapshot"` // the copy of its pre-mutation bytes
	Sum  string      `json:"sha256"`   // sha256 of those bytes
	Mode fs.FileMode `json:"mode"`
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// takeSnapshot copies file into dir and records its checksum and mode.
func takeSnapshot(dir, file string) (snapshot, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return snapshot{}, err
	}
	info, err := os.Stat(file)
	if err != nil {
		return snapshot{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return snapshot{}, err
	}
	s := snapshot{
		File: file,
		Path: filepath.Join(dir, sum(b)[:16]+"-"+filepath.Base(file)),
		Sum:  sum(b),
		Mode: info.Mode().Perm(),
	}
	if err := os.WriteFile(s.Path, b, 0o644); err != nil {
		return snapshot{}, err
	}
	return s, nil
}

// restore puts the pre-mutation bytes back and proves it did, by re-reading the
// file and checking it against the recorded checksum. A mismatch is an error
// and never a silent success: the whole tree's trustworthiness rests here.
func (s snapshot) restore() error {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return fmt.Errorf("snapshot unreadable, %s may still be mutated: %w", s.File, err)
	}
	if err := os.WriteFile(s.File, b, s.Mode); err != nil {
		return fmt.Errorf("restoring %s: %w", s.File, err)
	}
	back, err := os.ReadFile(s.File)
	if err != nil {
		return fmt.Errorf("re-reading %s after restore: %w", s.File, err)
	}
	if got := sum(back); got != s.Sum {
		return fmt.Errorf("%s did not restore: sha256 %s, want %s", s.File, got, s.Sum)
	}
	return nil
}

// discard removes the snapshot copy once the file it describes is verified back
// in place.
func (s snapshot) discard() error { return os.Remove(s.Path) }

// writeJournal records an in-flight mutation before the file is touched, so a
// harness that is killed between the mutation and the restore leaves a record
// naming exactly what to put back. Without it an interrupted run is
// indistinguishable from an author's own uncommitted edit — the same confusion
// that rules out `git checkout` as the restore.
func writeJournal(path string, s snapshot) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
