package cli

import (
	"path/filepath"
)

// canonicalPath makes a path absolute and symlink-resolved. The tail that does
// not exist yet (a not-yet-minted repository) is resolved as far up as it can
// be and the remainder joined back on verbatim, so the same directory always
// canonicalises to the same string whether or not its last components exist.
//
// This is the single canonical form project add --repo-path and bind store, so
// a path computed here must byte-match what those verbs persist or the two
// never reconcile for one directory.
func canonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return resolveSymlinks(abs)
}

// resolveSymlinks walks up from p until EvalSymlinks succeeds, then rejoins
// the unresolved tail. The filesystem root always exists, so the walk always
// terminates.
func resolveSymlinks(p string) (string, error) {
	head, tail := p, ""
	for {
		resolved, err := filepath.EvalSymlinks(head)
		if err == nil {
			if tail == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, tail), nil
		}
		parent := filepath.Dir(head)
		if parent == head {
			return p, nil
		}
		tail = filepath.Join(filepath.Base(head), tail)
		head = parent
	}
}
