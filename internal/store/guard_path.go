package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrRealStoreRefused reports an attempt to open the real store from a build
// that is not the cutover build.
var ErrRealStoreRefused = errors.New("real store refused")

// realStorePath is the one path the development build refuses. $DROPS_DB is not
// consulted: the point is to protect this exact file however it was named.
func realStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".drops", "drops.db"), nil
}

func guardPath(path string) error {
	if realStoreAllowed {
		return nil
	}
	real, err := realStorePath()
	if err != nil {
		// No home directory means no real store to protect.
		return nil
	}
	if samePath(path, real) {
		return fmt.Errorf("%w: %s belongs to the old build until cutover; set DROPS_DB to a scratch store",
			ErrRealStoreRefused, real)
	}
	return nil
}

// samePath compares two paths as filesystem locations. Both are made absolute
// and cleaned first, so a relative or dot-laden spelling cannot slip past.
func samePath(candidate, protected string) bool {
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		absolute = filepath.Clean(candidate)
	}
	if absolute == filepath.Clean(protected) {
		return true
	}
	// A symlinked home is still the same file. EvalSymlinks fails on a path
	// that does not exist yet, which is not a match by itself.
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return false
	}
	protectedResolved, err := filepath.EvalSymlinks(filepath.Clean(protected))
	if err != nil {
		return false
	}
	return resolved == protectedResolved
}
