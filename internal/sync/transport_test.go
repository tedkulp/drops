package sync

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/lockfile"
)

// heldLocker is the "another process is already syncing" case: AcquireWait
// gives up and reports ErrHeld.
type heldLocker struct{ waited time.Duration }

func (l *heldLocker) Acquire(string) (*lockfile.Lock, error) { return nil, lockfile.ErrHeld }
func (l *heldLocker) AcquireWait(_ string, timeout time.Duration) (*lockfile.Lock, error) {
	l.waited = timeout
	return nil, lockfile.ErrHeld
}

// unusableLocker is the other flock outcome: the filesystem cannot lock at all,
// which is not the same as somebody holding the lock.
type unusableLocker struct{}

func (unusableLocker) Acquire(string) (*lockfile.Lock, error) {
	return nil, errors.New("operation not supported")
}
func (unusableLocker) AcquireWait(string, time.Duration) (*lockfile.Lock, error) {
	return nil, errors.New("operation not supported")
}

func TestNewFillsInTheProductionDefaults(t *testing.T) {
	transport, opened := openTransport(t)

	if transport.remote != "origin" {
		t.Errorf("remote = %q, want %q", transport.remote, "origin")
	}
	if transport.branch != "main" {
		t.Errorf("branch = %q, want %q", transport.branch, "main")
	}
	if _, ok := transport.locker.(lockfile.FlockLocker); !ok {
		t.Errorf("locker = %T, want lockfile.FlockLocker", transport.locker)
	}
	dir := filepath.Dir(opened.Path())
	if transport.Dir() != dir {
		t.Errorf("Dir() = %q, want the store's directory %q", transport.Dir(), dir)
	}
	if got, want := transport.lockPath(), filepath.Join(dir, lockFileName); got != want {
		t.Errorf("lockPath() = %q, want it beside the store at %q", got, want)
	}
}

func TestNewKeepsAnExplicitRemoteAndBranch(t *testing.T) {
	transport, _ := openTransportWith(t, Options{Remote: "upstream", Branch: "trunk", Locker: unusableLocker{}})

	if transport.remote != "upstream" {
		t.Errorf("remote = %q, want the configured %q", transport.remote, "upstream")
	}
	if transport.branch != "trunk" {
		t.Errorf("branch = %q, want the configured %q", transport.branch, "trunk")
	}
	if _, ok := transport.locker.(unusableLocker); !ok {
		t.Errorf("locker = %T, want the injected locker", transport.locker)
	}
}

// TestWithLockReportsAHeldLockAsErrSyncLocked covers the distinction the flock
// contract exists for: held means another process is syncing, and an explicit
// sync says so rather than proceeding.
func TestWithLockReportsAHeldLockAsErrSyncLocked(t *testing.T) {
	locker := &heldLocker{}
	transport, _ := openTransportWith(t, Options{Locker: locker})

	release, locked, err := transport.withLock(t.Context())
	if !errors.Is(err, ErrSyncLocked) {
		t.Fatalf("withLock = %v, want ErrSyncLocked", err)
	}
	if locked {
		t.Error("withLock reported the lock as held by us")
	}
	if release != nil {
		t.Error("withLock returned a release function for a lock it never took")
	}
	if locker.waited != lockWait {
		t.Errorf("waited %v, want lockWait %v", locker.waited, lockWait)
	}
}

// TestWithLockProceedsUnlockedWhenTheFilesystemCannotLock is the other half:
// an unusable lock is not a held lock, so the sync proceeds and reports that it
// ran unlocked.
func TestWithLockProceedsUnlockedWhenTheFilesystemCannotLock(t *testing.T) {
	transport, _ := openTransportWith(t, Options{Locker: unusableLocker{}})

	release, locked, err := transport.withLock(t.Context())
	if err != nil {
		t.Fatalf("withLock = %v, want it to proceed", err)
	}
	if locked {
		t.Error("withLock reported a lock it could not take as held")
	}
	if release == nil {
		t.Fatal("withLock returned no release function")
	}
	if err := release(); err != nil {
		t.Errorf("release() = %v, want nil", err)
	}
}

func TestIsPushRejectedRecognisesOnlyARejection(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reject bool
	}{
		{"non-fast-forward", &gitx.CommandError{Stderr: "! [rejected] main -> main (non-fast-forward)"}, true},
		{"rejected", &gitx.CommandError{Stderr: "! [remote rejected] main -> main"}, true},
		{"fetch first", &gitx.CommandError{Stderr: "hint: fetch first and integrate the remote changes"}, true},
		{"mixed case", &gitx.CommandError{Stderr: "Non-Fast-Forward"}, true},
		{"permission denied", &gitx.CommandError{Stderr: "fatal: Could not read from remote repository"}, false},
		{"not a command error", errors.New("! [rejected] non-fast-forward"), false},
		{"no error", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPushRejected(tt.err); got != tt.reject {
				t.Fatalf("isPushRejected(%v) = %v, want %v", tt.err, got, tt.reject)
			}
		})
	}
}
