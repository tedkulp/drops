package sync

import (
	"fmt"
	"os"
	"path/filepath"
)

// fileMode is every transport file's permission. The mirror is a plaintext copy
// of every issue and memory in the store, so the ambient umask is not trusted:
// each file is chmod'd to 0600 after it lands, never left to whatever mode its
// temp happened to inherit.
const fileMode = 0o600

// tempName names the in-progress file so a crash mid-write can never leave a
// stageable artifact under the final name.
func tempName(name string) string { return "." + name + ".tmp" }

// writeFileAtomic replaces name inside dir at fileMode. Rename within a
// directory is atomic, so a crash at any point leaves either the old file or the
// new one, never a truncated one. The temp is fsync'd before the rename and the
// directory after, so the contents and the directory entry are both durable.
func writeFileAtomic(dir, name string, body []byte) error {
	tmp := filepath.Join(dir, tempName(name))
	final := filepath.Join(dir, name)

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	fail := func(format string, args ...any) error {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf(format, args...)
	}
	if _, err := f.Write(body); err != nil {
		return fail("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		return fail("fsync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", name, err)
	}
	// Chmod after the rename, not just at create: a pre-existing temp keeps its
	// own mode through O_CREATE|O_TRUNC, and that mode rides the rename onto the
	// final path. This is the load-bearing guard.
	if err := os.Chmod(final, fileMode); err != nil {
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return fsyncDir(dir)
}

// fsyncDir makes a rename durable: the file bytes are on disk but the directory
// entry pointing at them is not guaranteed until the directory is synced. A
// power loss without it can resurrect the old name.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", dir, err)
	}
	return nil
}
