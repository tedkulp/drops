package sync

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/lockfile"
	"github.com/tedkulp/drops/internal/store"
)

// ErrSyncLocked means an explicit sync could not take the advisory lock within
// its wait: another process is already flushing this store.
var ErrSyncLocked = errors.New("another process is syncing")

// lockWait bounds how long an explicit sync waits for the lock before reporting
// ErrSyncLocked. Polling rather than a blocking flock keeps a stuck holder from
// hanging a session close forever.
const lockWait = 5 * time.Second

// Options are the transport's collaborators, each with a production default.
type Options struct {
	// Git is the context-bounded client; its zero value is ready to use.
	Git gitx.Client
	// Locker serialises syncs across processes; nil selects a flock locker.
	Locker lockfile.Locker
	// Remote is the remote name (default "origin"), URL its address set when
	// non-empty, and Branch the branch (default "main").
	Remote string
	URL    string
	Branch string
}

// Transport coordinates the two-machine transport over one store, its core
// rules, a Git client, and a lock.
type Transport struct {
	core   *core.Core
	store  *store.Store
	git    gitx.Client
	locker lockfile.Locker
	remote string
	url    string
	branch string
	dir    string
}

// New builds a transport. The store directory is derived from the store's path.
func New(rules *core.Core, opened *store.Store, opts Options) *Transport {
	t := &Transport{
		core:   rules,
		store:  opened,
		git:    opts.Git,
		locker: opts.Locker,
		remote: opts.Remote,
		url:    opts.URL,
		branch: opts.Branch,
		dir:    filepath.Dir(opened.Path()),
	}
	if t.locker == nil {
		t.locker = lockfile.FlockLocker{}
	}
	if t.remote == "" {
		t.remote = "origin"
	}
	if t.branch == "" {
		t.branch = "main"
	}
	return t
}

// Dir reports the store directory the transport writes its files into.
func (t *Transport) Dir() string { return t.dir }

func (t *Transport) lockPath() string { return filepath.Join(t.dir, lockFileName) }

// withLock acquires the advisory lock, waiting up to lockWait. It returns a
// release function and whether the lock is actually held; when the filesystem
// cannot lock at all it proceeds unlocked and reports that fact so the caller
// can surface it, per the flock contract (held vs unusable are different).
func (t *Transport) withLock(ctx context.Context) (release func() error, locked bool, err error) {
	lock, err := t.locker.AcquireWait(t.lockPath(), lockWait)
	if err == nil {
		return lock.Release, true, nil
	}
	if errors.Is(err, lockfile.ErrHeld) {
		return nil, false, fmt.Errorf("%w: %s is held", ErrSyncLocked, t.lockPath())
	}
	return func() error { return nil }, false, nil // unusable, not held: proceed and warn
}

// ensureRepo hardens the store directory as a git repository and points origin
// at the configured URL.
func (t *Transport) ensureRepo(ctx context.Context) error {
	if err := t.git.Ensure(ctx, gitx.RepositoryConfig{Dir: t.dir}); err != nil {
		return fmt.Errorf("ensure Git repository: %w", err)
	}
	if t.url != "" {
		if err := t.git.SetRemoteURL(ctx, t.dir, t.remote, t.url); err != nil {
			return fmt.Errorf("configure Git remote: %w", err)
		}
	}
	return nil
}
