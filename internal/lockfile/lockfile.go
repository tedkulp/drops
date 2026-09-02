package lockfile

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// ErrHeld means another process holds the lock. It is deliberately not
// returned for any other failure: a caller distinguishes "someone else is
// already doing this work, skip" from "the lock is unusable here, proceed
// without it and warn".
var ErrHeld = errors.New("lock is held by another process")

// Lock is a held advisory lock. Release it to drop the flock and close the
// underlying descriptor.
type Lock struct {
	f    *os.File
	done atomic.Bool
}

// classify maps a flock(2) failure to the error Acquire returns. EWOULDBLOCK
// — held by another process's open file description — becomes ErrHeld.
// Everything else, including ENOTSUP/EOPNOTSUPP on a filesystem that does not
// implement flock (NFS, some FUSE mounts), passes through unchanged so a
// caller can tell it apart from ErrHeld.
func classify(flockErr error) error {
	if errors.Is(flockErr, unix.EWOULDBLOCK) {
		return ErrHeld
	}
	return flockErr
}

// Acquire takes the lock without blocking, creating path if it is absent.
func Acquire(path string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	// O_CREATE applies the ambient umask, so a file created here needs its
	// mode set again to match the 0600 store mirror it sits beside.
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, fmt.Errorf("chmod lock %s: %w", path, err)
	}

	if flockErr := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); flockErr != nil {
		f.Close()
		err := classify(flockErr)
		if errors.Is(err, ErrHeld) {
			return nil, ErrHeld
		}
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// AcquireWait polls until the lock frees or timeout elapses, then reports
// ErrHeld.
//
// Polling rather than a blocking flock: a blocking LOCK_EX has no timeout, and
// the explicit sync path needs a bounded wait so a stuck holder cannot hang a
// session close forever. The interval is short enough to be imperceptible and
// long enough not to spin.
func AcquireWait(path string, timeout time.Duration) (*Lock, error) {
	const interval = 25 * time.Millisecond
	deadline := time.Now().Add(timeout)
	for {
		l, err := Acquire(path)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, ErrHeld) {
			return nil, err // unusable, not held
		}
		if !time.Now().Before(deadline) {
			return nil, ErrHeld
		}
		time.Sleep(interval)
	}
}

// Release drops the lock and closes the descriptor. Safe to call more than
// once, including concurrently: the atomic compare-and-swap makes two racing
// Release calls agree on one winner instead of double-closing the fd. The
// kernel drops the flock when the process exits regardless, so a crash
// mid-sync leaves no stale lock to clean up — the reason an O_EXCL lock file
// was not used: that kind survives a crash and turns a transient degradation
// into a permanent one needing manual cleanup.
func (l *Lock) Release() error {
	if l == nil || !l.done.CompareAndSwap(false, true) {
		return nil
	}
	// Unlock explicitly rather than relying on close alone: it makes the
	// intent readable, and the close below drops the kernel's reference.
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	if err := l.f.Close(); err != nil {
		return fmt.Errorf("close lock: %w", err)
	}
	return nil
}
