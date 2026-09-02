package sync_test

import (
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/sync"
)

func strPtr(s string) *string { return &s }

func (m *machine) replicaKey(t *testing.T) model.ReplicaKey {
	t.Helper()
	sidecar, err := sync.LoadSidecar(m.dir)
	if err != nil {
		t.Fatalf("load sidecar: %v", err)
	}
	if sidecar == nil {
		t.Fatal("no sidecar")
	}
	return sidecar.Replica
}

func TestRoundTripBothDirections(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	issueA, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from A", Type: model.TypeTask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("A sync: %v", err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatalf("B sync: %v", err)
	}

	issueB, err := b.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "from B", Type: model.TypeTask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("A sync: %v", err)
	}

	if got, err := a.store.Issue(t.Context(), issueB.ID); err != nil || got.Title != "from B" {
		t.Fatalf("A issue from B = %q/%v, want %q", got.Title, err, "from B")
	}
	if got, err := b.store.Issue(t.Context(), issueA.ID); err != nil || got.Title != "from A" {
		t.Fatalf("B issue from A = %q/%v, want %q", got.Title, err, "from A")
	}
}

func TestConcurrentEditConvergesDeterministically(t *testing.T) {
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

	// Both machines edit the same issue from the same observed generation.
	if _, err := a.core.EditIssue(t.Context(), created.ID, core.IssueEdit{Title: strPtr("from A")}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.core.EditIssue(t.Context(), created.ID, core.IssueEdit{Title: strPtr("from B")}); err != nil {
		t.Fatal(err)
	}

	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("A sync: %v", err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatalf("B sync: %v", err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("A sync: %v", err)
	}

	gotA, err := a.store.Issue(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := b.store.Issue(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotA.Title != gotB.Title {
		t.Fatalf("divergent titles after sync: A=%q B=%q", gotA.Title, gotB.Title)
	}

	keyA, keyB := a.replicaKey(t), b.replicaKey(t)
	want := "from B"
	if keyA > keyB {
		want = "from A"
	}
	if gotA.Title != want {
		t.Fatalf("winner = %q, want %q (A key %s vs B key %s)", gotA.Title, want, keyA, keyB)
	}
}

func TestTombstoneFlowsToTheOtherMachine(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)
	b := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	created, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "doomed", Type: model.TypeTask})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	if _, err := a.core.SetIssueTombstone(t.Context(), created.ID, model.Tombstoned); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.transport.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}

	got, err := b.store.Issue(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tombstone != model.Tombstoned {
		t.Fatalf("B issue tombstone = %v, want tombstoned", got.Tombstone)
	}
}

func TestRekeyRotatesTheActiveKey(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)

	before := a.replicaKey(t)
	next, err := a.transport.Rekey(t.Context())
	if err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if next == before {
		t.Fatalf("Rekey returned the same key %q", next)
	}
	if got := a.replicaKey(t); got != next {
		t.Fatalf("sidecar key = %q, want rotated key %q", got, next)
	}

	successions, err := a.core.ReplicaSuccessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, succession := range successions {
		if succession.OldReplica == before && succession.NewReplica == next {
			found = true
		}
	}
	if !found {
		t.Fatalf("no succession %s -> %s recorded", before, next)
	}
}
