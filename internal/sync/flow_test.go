package sync_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/sync"
)

// TestImportNeverChangesTheActiveKey: replica identity is machine-local, so a
// store that merges another machine's snapshot keeps authoring under its own
// key. Importing one would give two installations the same identity, which is
// the fork this whole subsystem exists to make impossible.
func TestImportNeverChangesTheActiveKey(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from A", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	before := b.replicaKey(t)
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if after := b.replicaKey(t); after != before {
		t.Fatalf("B replica key = %q after importing, want the unchanged %q", after, before)
	}
	if after := b.replicaKey(t); after == a.replicaKey(t) {
		t.Fatal("B adopted A's replica key")
	}
}

// TestASyncWithNothingLocalExportsNothing: an export is driven by unexported
// local writes, not by running the command. A store already in step with its
// mirror pushes nothing.
func TestASyncWithNothingLocalExportsNothing(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)

	if first, err := a.transport.Sync(t.Context()); err != nil || !first.Exported {
		t.Fatalf("first sync = (%+v, %v), want the bootstrap exported", first, err)
	}
	second, err := a.transport.Sync(t.Context())
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.Exported {
		t.Error("second sync exported with no local writes")
	}
	if second.Imported {
		t.Error("second sync imported its own head back")
	}
}

// TestAPurePullRecordsTheHeadItFastForwardedTo: after a pull with nothing local
// to send, the store and its sidecar name the remote tip, so the next local
// write builds on the shared history instead of re-exporting from behind it.
func TestAPurePullRecordsTheHeadItFastForwardedTo(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	// Get both machines past their bootstrap export so B's next sync is a pure
	// pull: nothing local, something remote.
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	key := a.projectKey(t, "beacon")
	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from A", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	pull, err := b.transport.Sync(t.Context())
	if err != nil {
		t.Fatalf("B pull: %v", err)
	}
	if !pull.Imported || pull.Exported {
		t.Fatalf("B pull = %+v, want an import and no export", pull)
	}
	if pull.Head == "" {
		t.Fatal("B pull reported no head")
	}

	state, err := b.store.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.LastExportHead != pull.Head {
		t.Errorf("store last export head = %q, want the pulled head %q", state.LastExportHead, pull.Head)
	}
	sidecar, err := sync.LoadSidecar(b.dir)
	if err != nil || sidecar == nil {
		t.Fatalf("LoadSidecar = (%v, %v)", sidecar, err)
	}
	if sidecar.LastExportHead != pull.Head {
		t.Errorf("sidecar last export head = %q, want the pulled head %q", sidecar.LastExportHead, pull.Head)
	}
	// The branch itself has to move too, or the next local write commits behind
	// the shared history and is rejected on push.
	if head := strings.TrimSpace(runGit(t, b.dir, "rev-parse", "HEAD")); head != pull.Head {
		t.Errorf("B git head = %q, want the fast-forwarded %q", head, pull.Head)
	}

	// The pair is coherent, so the next local write can still author.
	bKey := b.projectKey(t, "site-b")
	if _, err := b.core.CreateIssue(t.Context(), core.CreateIssue{Project: bKey, Title: "from B", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatalf("B sync after a pure pull: %v", err)
	}
}

// TestSyncReportsWhatItMerged: a higher-generation replacement is routine and
// reported as counts. The counts are deterministic here because A's edit is
// strictly newer than what B holds, so nothing depends on the replica-key
// tie-break.
func TestSyncReportsWhatItMerged(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	created, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "base", Type: model.TypeTask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := a.core.EditIssue(t.Context(), created.ID, core.IssueEdit{Title: strPtr("from A")}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	result, err := b.transport.Sync(t.Context())
	if err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if !result.Imported {
		t.Fatal("B sync did not import A's newer edit")
	}
	if result.Applied == 0 {
		t.Errorf("B sync = %+v, want the newer revision counted as applied", result)
	}
	if result.Ignored == 0 {
		t.Errorf("B sync = %+v, want the unchanged records counted as ignored", result)
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("B sync reported conflicts %+v for a strictly newer revision", result.Conflicts)
	}
	if got, err := b.store.Issue(t.Context(), created.ID); err != nil || got.Title != "from A" {
		t.Fatalf("B issue = (%q, %v), want the applied edit", got.Title, err)
	}
}

// TestSyncReportsTheConflictsItResolved: an equal-generation resolution is
// reported, not silently applied, because an automatic sync has to be able to
// warn about it. Which side wins is the replica-key tie-break and is not
// asserted here; TestConcurrentEditConvergesDeterministically covers that.
func TestSyncReportsTheConflictsItResolved(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	created, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "base", Type: model.TypeTask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := a.core.EditIssue(t.Context(), created.ID, core.IssueEdit{Title: strPtr("from A")}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.core.EditIssue(t.Context(), created.ID, core.IssueEdit{Title: strPtr("from B")}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	result, err := b.transport.Sync(t.Context())
	if err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if len(result.Conflicts) == 0 {
		t.Fatal("B sync reported no conflict for two edits at the same generation")
	}
	found := false
	for _, conflict := range result.Conflicts {
		if conflict.Record.Kind == model.RecordIssue && conflict.Record.Key == string(created.ID) {
			found = true
		}
	}
	if !found {
		t.Errorf("conflicts %+v do not name the contested issue %s", result.Conflicts, created.ID)
	}
}

// TestSyncReportsARunItCouldNotLock: an unusable lock is not a held lock, so
// the sync proceeds and says so rather than refusing.
func TestSyncReportsARunItCouldNotLock(t *testing.T) {
	remote := newRemote(t)
	a := newMachineWithOptions(t, sync.Options{URL: remote, Locker: unlockableLocker{}})

	result, err := a.transport.Sync(t.Context())
	if err != nil {
		t.Fatalf("Sync with an unusable lock: %v", err)
	}
	if !result.Unlocked {
		t.Error("Sync did not report that it ran without the lock")
	}
	if !result.Exported {
		t.Error("Sync with an unusable lock did not proceed to export")
	}
}

// TestSyncRefusesWhileAnotherProcessHoldsTheLock is the other half of the flock
// contract: held means skip, and an explicit sync says so.
func TestSyncRefusesWhileAnotherProcessHoldsTheLock(t *testing.T) {
	remote := newRemote(t)
	a := newMachineWithOptions(t, sync.Options{URL: remote, Locker: busyLocker{}})

	if _, err := a.transport.Sync(t.Context()); !errors.Is(err, sync.ErrSyncLocked) {
		t.Fatalf("Sync against a held lock = %v, want ErrSyncLocked", err)
	}
}

// TestAMergedExportNamesBothParents: an export made after folding in a remote
// snapshot names both the head it built on and the head it merged. Those
// predecessors are the entire basis of the other machine's fork proof, so
// losing one turns a linear advance into a fork on the far side.
func TestAMergedExportNamesBothParents(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	// B goes first so it has a head of its own, then A lands a write on top of
	// it. B's next sync therefore has both a parent and something to merge.
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	key := a.projectKey(t, "beacon")
	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from A", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	remoteBefore := strings.TrimSpace(runGit(t, "", "--git-dir="+remote, "rev-parse", "refs/heads/main"))
	myHeadBefore := strings.TrimSpace(runGit(t, b.dir, "rev-parse", "HEAD"))
	if myHeadBefore == "" || myHeadBefore == remoteBefore {
		t.Fatalf("B head %q and remote head %q must differ for this to be a merge", myHeadBefore, remoteBefore)
	}
	bKey := b.projectKey(t, "site-b")
	if _, err := b.core.CreateIssue(t.Context(), core.CreateIssue{Project: bKey, Title: "from B", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}

	result, err := b.transport.Sync(t.Context())
	if err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if !result.Imported || !result.Exported {
		t.Fatalf("B sync = %+v, want both an import and an export", result)
	}

	raw, err := os.ReadFile(filepath.Join(b.dir, "mirror.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := mirror.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(snapshot.Predecessors, remoteBefore) {
		t.Errorf("predecessors %q do not name the merged remote head %q", snapshot.Predecessors, remoteBefore)
	}
	if !slices.Contains(snapshot.Predecessors, myHeadBefore) {
		t.Errorf("predecessors %q do not name B's own head %q", snapshot.Predecessors, myHeadBefore)
	}

	// The Git commit has to carry the same two parents, or the push is not a
	// fast-forward and the remote refuses it.
	parents := strings.Fields(runGit(t, b.dir, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 {
		t.Fatalf("HEAD parents = %q, want a two-parent merge commit", parents)
	}
	if !slices.Contains(parents[1:], remoteBefore) {
		t.Errorf("commit parents %q do not include the merged remote head %q", parents[1:], remoteBefore)
	}
	if !slices.Contains(parents[1:], myHeadBefore) {
		t.Errorf("commit parents %q do not include B's own head %q", parents[1:], myHeadBefore)
	}
}
