package sync_test

import (
	"errors"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
	"github.com/tedkulp/drops/internal/sync"
)

// TestCoherenceMismatchRefusesExport reproduces a torn restore: the sidecar and
// the database remember different last export heads, so exporting must refuse
// rather than author locally under a stale identity.
func TestCoherenceMismatchRefusesExport(t *testing.T) {
	remote := newRemote(t)
	a := newMachine(t, remote)

	key := a.projectKey(t, "beacon")
	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "first", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Tamper the sidecar's remembered head to simulate a differing restore.
	sidecar, err := sync.LoadSidecar(a.dir)
	if err != nil {
		t.Fatal(err)
	}
	sidecar.LastExportHead = "0000000000000000000000000000000000000000"
	if err := sidecar.Save(a.dir); err != nil {
		t.Fatal(err)
	}

	if _, err := a.core.CreateIssue(t.Context(), core.CreateIssue{Project: key, Title: "second", Type: model.TypeTask}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.transport.Sync(t.Context()); !errors.Is(err, sync.ErrExportHeadMismatch) {
		t.Fatalf("Sync after coherence mismatch = %v, want ErrExportHeadMismatch", err)
	}
}

// TestRekeyWithoutActiveKeyRefuses covers the no-key refusal: a store that has
// never minted a replica has nothing to rotate.
func TestRekeyWithoutActiveKeyRefuses(t *testing.T) {
	dir := t.TempDir()
	opened, err := store.Open(t.Context(), dir+"/drops.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })

	rules := core.New(opened, "", nil, nil, nil)
	if err := rules.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	transport := sync.New(rules, opened, sync.Options{})

	if _, err := transport.Rekey(t.Context()); !errors.Is(err, sync.ErrNoActiveKey) {
		t.Fatalf("Rekey without a key = %v, want ErrNoActiveKey", err)
	}
}
