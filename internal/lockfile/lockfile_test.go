package lockfile_test

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/lockfile"
)

func TestAcquireAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	l, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("lock file mode = %04o, want 0600", perm)
	}
	if err := l.Release(); err != nil {
		t.Errorf("Release: %v", err)
	}
	// Re-acquirable after release: flock released, fd closed.
	l2, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatalf("re-Acquire: %v", err)
	}
	l2.Release()
}

// Release must be a no-op when called twice (or on a nil lock), not a
// double-close of the descriptor.
func TestReleaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	l, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
	var nilLock *lockfile.Lock
	if err := nilLock.Release(); err != nil {
		t.Fatalf("nil Release: %v", err)
	}
}

// startHolder runs the holder helper as a real second process and returns its
// stdin pipe once it reports the lock taken. Closing the pipe releases the
// lock. Synchronising on the child's output rather than a sleep keeps the test
// independent of the scheduler. extraArgs pass through (see testdata/holder
// for the optional release-delay argument).
func startHolder(t *testing.T, path string, extraArgs ...string) io.WriteCloser {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "holder")
	build := exec.Command("go", "build", "-o", bin, "./testdata/holder")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build holder: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, append([]string{path}, extraArgs...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	t.Cleanup(func() {
		stdin.Close()
		cmd.Wait()
	})
	if !bufio.NewScanner(stdout).Scan() {
		t.Fatal("holder never reported the lock taken")
	}
	return stdin
}

// The distinction the whole package exists for: ErrHeld means another process
// is already doing this work and the caller should skip; any other error means
// the lock cannot be used and the caller must proceed WITHOUT it — a store
// that never serialises while reporting success is worse than one that
// serialises without mutual exclusion.
func TestAcquireReportsHeldDistinctly(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	startHolder(t, path)

	if _, err := lockfile.Acquire(path); !errors.Is(err, lockfile.ErrHeld) {
		t.Fatalf("Acquire = %v, want ErrHeld", err)
	}
}

// O_CREATE only applies its mode bits when it creates the file, so a
// pre-existing lock file with the wrong permissions needs its own repair path.
func TestAcquireRepairsPermissionsOnAPreExistingLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	if err := os.WriteFile(path, nil, 0o666); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatalf("chmod setup: %v", err)
	}

	l, err := lockfile.Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Release()

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("lock file mode = %04o, want 0600 (Acquire must repair a pre-existing file)", perm)
	}
}

// A path that cannot be opened at all must NOT look like a held lock, or a
// caller would skip the sync forever instead of proceeding unlocked.
func TestAcquireDistinguishesAnUnusableLockFromAHeldOne(t *testing.T) {
	base := t.TempDir()
	notDir := filepath.Join(base, "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := lockfile.Acquire(filepath.Join(notDir, ".sync.lock"))
	if err == nil {
		t.Fatal("Acquire succeeded on an unusable path")
	}
	if errors.Is(err, lockfile.ErrHeld) {
		t.Error("an unusable lock reported as ErrHeld; the caller would skip forever instead of proceeding and warning")
	}
}

func TestAcquireWaitTimesOutRatherThanBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	startHolder(t, path)

	if _, err := lockfile.AcquireWait(path, 300*time.Millisecond); !errors.Is(err, lockfile.ErrHeld) {
		t.Fatalf("AcquireWait = %v, want ErrHeld after the timeout", err)
	}
}

// This test exercises the retry loop itself: the holder keeps the lock for a
// guaranteed 300ms after its stdin closes (see testdata/holder), which is more
// than 10 poll intervals, so AcquireWait's first several attempts MUST fail
// with ErrHeld before the holder releases and a later attempt succeeds. The
// delay is what makes this deterministic: without it, closing stdin and the
// kernel dropping the flock have no guaranteed order, and the first attempt
// might succeed anyway even with the retry loop deleted.
func TestAcquireWaitSucceedsOnceTheHolderExits(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".sync.lock")
	stdin := startHolder(t, path, "300")

	if err := stdin.Close(); err != nil {
		t.Fatalf("close holder stdin: %v", err)
	}
	l, err := lockfile.AcquireWait(path, 10*time.Second)
	if err != nil {
		t.Fatalf("AcquireWait: %v", err)
	}
	l.Release()
}

func TestFlockLockerSatisfiesTheLockerInterface(t *testing.T) {
	var locker lockfile.Locker = lockfile.FlockLocker{}

	path := filepath.Join(t.TempDir(), ".sync.lock")
	l, err := locker.Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer l.Release()

	// Held through the interface, not just through the package functions.
	if _, err := locker.Acquire(path); !errors.Is(err, lockfile.ErrHeld) {
		t.Fatalf("Acquire = %v, want ErrHeld", err)
	}
}
