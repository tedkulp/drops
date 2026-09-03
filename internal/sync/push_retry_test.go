package sync_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// installPushShim puts a `git` on PATH that stands in for the other machine
// landing a push first. Every invocation but the first `push` is the real git,
// so the repository work is real; the first push instead moves the remote
// branch to advanceTo (when non-empty) and reports the rejection Git would.
//
// This is the one race the transport exists to survive and the one that cannot
// be staged by ordering two real syncs: the remote has to advance between this
// machine's fetch and its push.
func installPushShim(t *testing.T, remote, advanceTo string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	shim := t.TempDir()
	advance := ":"
	if advanceTo != "" {
		advance = realGit + " --git-dir=" + remote + " update-ref refs/heads/main " + advanceTo
	}
	script := strings.Join([]string{
		"#!/bin/sh",
		"for arg in \"$@\"; do",
		"  if [ \"$arg\" = push ]; then",
		"    if [ ! -e " + shim + "/rejected ]; then",
		"      : > " + shim + "/rejected",
		"      " + advance,
		"      echo ' ! [rejected] HEAD -> main (non-fast-forward)' >&2",
		"      exit 1",
		"    fi",
		"    break",
		"  fi",
		"done",
		"exec " + realGit + " \"$@\"",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRejectedPushRePullsAndRetries: a concurrent advance landing behind us
// must be folded in and pushed again, not reported as a failure. The proof is
// that the other machine's second issue is in this store afterwards — it can
// only have arrived through the retry's own pull.
func TestRejectedPushRePullsAndRetries(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from A", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("A sync: %v", err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatalf("B sync: %v", err)
	}
	shared := remoteMain(t, remote)

	// A lands a second write. Its push is then rewound out of the remote and
	// replayed by the shim at the moment B pushes, which is the one interleaving
	// two ordered syncs cannot stage.
	issueA2, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from A again", Type: model.TypeTask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("A second sync: %v", err)
	}
	advanced := remoteMain(t, remote)
	if advanced == shared {
		t.Fatal("A's second sync did not advance the remote")
	}
	runGit(t, "", "--git-dir="+remote, "update-ref", "refs/heads/main", shared)

	if _, err := b.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from B", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}

	installPushShim(t, remote, advanced)
	result, err := b.transport.Sync(t.Context())
	if err != nil {
		t.Fatalf("B sync over a rejected push: %v", err)
	}
	if !result.Exported {
		t.Error("B sync did not report an export")
	}

	if got, err := b.store.Issue(t.Context(), issueA2.ID); err != nil || got.Title != "from A again" {
		t.Fatalf("B issue from A's second write = (%q, %v), want it pulled in by the retry", got.Title, err)
	}
	if head := remoteMain(t, remote); head == advanced {
		t.Fatal("the remote never took B's retried push")
	}
}

func remoteMain(t *testing.T, remote string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, "", "--git-dir="+remote, "rev-parse", "refs/heads/main"))
}

// TestRejectedPushGivesUpWhenTheRemoteHasNotMoved: a rejection with no advance
// behind it is not something another pull can fix, so the retry stops instead of
// spinning.
func TestRejectedPushGivesUpWhenTheRemoteHasNotMoved(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from A", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}

	installPushShim(t, remote, "")
	if _, err := a.transport.Sync(t.Context()); err == nil {
		t.Fatal("A sync over an unexplained rejection = nil error, want the push failure reported")
	} else if !strings.Contains(err.Error(), "push") {
		t.Fatalf("A sync error = %v, want it to name the push", err)
	}
}
