package sync_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/lockfile"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
	"github.com/tedkulp/drops/internal/sync"
)

// machine is a test fixture: one store, its core rules, and its transport, wired
// the way a real process would — open the store, mint the sidecar, build core
// over that key, then build the transport over both.
type machine struct {
	dir       string
	store     *store.Store
	core      *core.Core
	transport *sync.Transport
}

type fixedIDSource struct{ next model.ID }

func (s *fixedIDSource) NewID(model.OwnerKind, *model.ID, []model.ID) (model.ID, error) {
	return s.next, nil
}

func newMachine(t *testing.T, remote string) *machine {
	t.Helper()
	return newMachineWithIDs(t, remote, nil)
}

func newMachineWithOptions(t *testing.T, opts sync.Options) *machine {
	t.Helper()
	return build(t, opts, nil)
}

func newMachineWithIDs(t *testing.T, remote string, ids core.IDSource) *machine {
	t.Helper()
	return build(t, sync.Options{URL: remote, Remote: "origin", Branch: "main"}, ids)
}

// busyLocker is another process already syncing this store; unlockableLocker is
// a filesystem that cannot lock at all. The flock contract keeps them apart, so
// the transport has to as well.
type busyLocker struct{}

func (busyLocker) Acquire(string) (*lockfile.Lock, error) { return nil, lockfile.ErrHeld }
func (busyLocker) AcquireWait(string, time.Duration) (*lockfile.Lock, error) {
	return nil, lockfile.ErrHeld
}

type unlockableLocker struct{}

func (unlockableLocker) Acquire(string) (*lockfile.Lock, error) {
	return nil, errors.New("operation not supported")
}
func (unlockableLocker) AcquireWait(string, time.Duration) (*lockfile.Lock, error) {
	return nil, errors.New("operation not supported")
}

func build(t *testing.T, opts sync.Options, ids core.IDSource) *machine {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "site")
	opened, err := store.Open(t.Context(), filepath.Join(dir, "drops.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })

	sidecar, err := sync.LoadSidecar(dir)
	if err != nil {
		t.Fatalf("load sidecar: %v", err)
	}
	if sidecar == nil {
		if sidecar, err = sync.MintSidecar(dir); err != nil {
			t.Fatalf("mint sidecar: %v", err)
		}
	}
	rules := core.New(opened, sidecar.Replica, nil, ids, nil)
	if err := rules.Bootstrap(t.Context()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	transport := sync.New(rules, opened, opts)
	return &machine{dir: dir, store: opened, core: rules, transport: transport}
}

// projectKey creates the named project and returns its immutable key.
func (m *machine) projectKey(t *testing.T, slug string) model.ProjectKey {
	t.Helper()
	project, err := m.core.CreateProject(t.Context(), slug)
	if err != nil {
		t.Fatalf("create project %s: %v", slug, err)
	}
	return project.Key
}

func TestFirstSyncMovesAnIssueToTheOtherMachine(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	created, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "sync me", Description: "body", Type: model.TypeTask})
	if err != nil {
		t.Fatalf("A create issue: %v", err)
	}

	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("A sync: %v", err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatalf("B sync: %v", err)
	}

	got, err := b.store.Issue(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("B read imported issue: %v", err)
	}
	if got.Title != "sync me" {
		t.Fatalf("B issue title = %q, want %q", got.Title, "sync me")
	}
}

// TestIdentityCollisionRefusesImport forces the same opaque id to be minted on
// two machines with different creation replicas. Sync must propagate core's hard
// identity collision rather than swallowing it.
func TestIdentityCollisionRefusesImport(t *testing.T) {
	remote := newRemote(t)
	a := newMachineWithIDs(t, remote, &fixedIDSource{next: "aaaaa"})
	b := newMachineWithIDs(t, remote, &fixedIDSource{next: "aaaaa"})

	key := a.projectKey(t, "beacon")
	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "mine", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	// B mints the same opaque id under its own key and project, then imports
	// A's copy of that id.
	bKey := b.projectKey(t, "beacon")
	if _, err := b.core.CreateIssue(t.Context(), core.CreateIssue{Project: bKey, Title: "theirs", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err == nil {
		t.Fatal("B sync = nil error, want identity collision")
	}
}

func newRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "init", "--bare", "-q", "-b", "main", remote)
	return remote
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
