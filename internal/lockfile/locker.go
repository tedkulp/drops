package lockfile

import "time"

// Locker serializes access to a named file across processes. sync accepts it
// so a test can inject a lock-acquisition failure without a filesystem that
// lacks flock support; production uses FlockLocker.
type Locker interface {
	// Acquire takes the lock without blocking, returning ErrHeld when another
	// process holds it.
	Acquire(path string) (*Lock, error)
	// AcquireWait blocks until the lock frees or timeout elapses.
	AcquireWait(path string, timeout time.Duration) (*Lock, error)
}

// FlockLocker is the real flock(2)-backed Locker. Its zero value is ready to
// use.
type FlockLocker struct{}

// Acquire implements Locker.
func (FlockLocker) Acquire(path string) (*Lock, error) { return Acquire(path) }

// AcquireWait implements Locker.
func (FlockLocker) AcquireWait(path string, timeout time.Duration) (*Lock, error) {
	return AcquireWait(path, timeout)
}

// FlockLocker is the one real adapter behind the Locker seam.
var _ Locker = FlockLocker{}
