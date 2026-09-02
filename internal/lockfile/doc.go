// Package lockfile provides advisory whole-file locking over flock(2).
//
// The difference between "another process holds the lock" and "the lock
// cannot be used here" is load-bearing, and collapsing the two produces a
// store that silently never serialises on a filesystem without flock support.
// ErrHeld means a caller should skip work another process is already doing;
// any other error means the filesystem cannot lock at all.
package lockfile
