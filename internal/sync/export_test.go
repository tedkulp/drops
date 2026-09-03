package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureSidecarMintsOnceThenReuses is the sidecar lifecycle: a store with no
// key mints one the first time it must author, and every later call reuses it
// rather than rotating identity behind the caller's back.
func TestEnsureSidecarMintsOnceThenReuses(t *testing.T) {
	transport, _ := openTransport(t)

	minted, err := transport.ensureSidecar()
	if err != nil {
		t.Fatalf("ensureSidecar: %v", err)
	}
	if minted.Replica == "" {
		t.Fatal("ensureSidecar minted an empty replica key")
	}
	again, err := transport.ensureSidecar()
	if err != nil {
		t.Fatalf("second ensureSidecar: %v", err)
	}
	if again.Replica != minted.Replica {
		t.Fatalf("second ensureSidecar = %q, want the reused key %q", again.Replica, minted.Replica)
	}
}

// TestEnsureSidecarRefusesAMalformedSidecar: a present but unreadable sidecar is
// a hard error, never quietly replaced with a fresh identity.
func TestEnsureSidecarRefusesAMalformedSidecar(t *testing.T) {
	transport, _ := openTransport(t)
	if err := os.WriteFile(filepath.Join(transport.dir, sidecarFileName), []byte("{"), fileMode); err != nil {
		t.Fatalf("write malformed sidecar: %v", err)
	}

	if sidecar, err := transport.ensureSidecar(); err == nil {
		t.Fatalf("ensureSidecar over a malformed sidecar = (%#v, nil), want a hard error", sidecar)
	}
	raw, err := os.ReadFile(filepath.Join(transport.dir, sidecarFileName))
	if err != nil || string(raw) != "{" {
		t.Fatalf("sidecar = (%q, %v), want the malformed file left untouched", raw, err)
	}
}

// TestCheckCoherenceComparesBothHeads: the store and the sidecar each remember
// the last export head, and only an equal pair may author locally.
func TestCheckCoherenceComparesBothHeads(t *testing.T) {
	transport, opened := openTransport(t)

	sidecar, err := transport.ensureSidecar()
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.checkCoherence(t.Context(), sidecar); err != nil {
		t.Fatalf("checkCoherence on a fresh pair = %v, want nil", err)
	}

	state, err := opened.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.core.MarkExported(t.Context(), state.WriteSeq, "0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatal(err)
	}
	// The store has moved on; the sidecar has not.
	if err := transport.checkCoherence(t.Context(), sidecar); !errors.Is(err, ErrExportHeadMismatch) {
		t.Fatalf("checkCoherence with only the store advanced = %v, want ErrExportHeadMismatch", err)
	}

	sidecar.LastExportHead = "0123456789abcdef0123456789abcdef01234567"
	if err := transport.checkCoherence(t.Context(), sidecar); err != nil {
		t.Fatalf("checkCoherence once both agree = %v, want nil", err)
	}

	// And the mirror image: the sidecar ahead of the store is equally a torn pair.
	sidecar.LastExportHead = "ffffffffffffffffffffffffffffffffffffffff"
	if err := transport.checkCoherence(t.Context(), sidecar); !errors.Is(err, ErrExportHeadMismatch) {
		t.Fatalf("checkCoherence with only the sidecar advanced = %v, want ErrExportHeadMismatch", err)
	}
}

// TestRecordExportStampsBothSides: the export mark in the database and the head
// in the sidecar are written together, because a coherent pair is what proves a
// restore has not been torn.
func TestRecordExportStampsBothSides(t *testing.T) {
	transport, opened := openTransport(t)

	const head = "abcabcabcabcabcabcabcabcabcabcabcabcabca"
	state, err := opened.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.recordExport(t.Context(), state.WriteSeq, head); err != nil {
		t.Fatalf("recordExport: %v", err)
	}

	after, err := opened.State(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after.LastExportHead != head {
		t.Errorf("store last export head = %q, want %q", after.LastExportHead, head)
	}
	if !after.Exported() {
		t.Errorf("store still reports unexported writes after recordExport")
	}
	sidecar, err := LoadSidecar(transport.dir)
	if err != nil || sidecar == nil {
		t.Fatalf("LoadSidecar = (%v, %v), want the stamped sidecar", sidecar, err)
	}
	if sidecar.LastExportHead != head {
		t.Errorf("sidecar last export head = %q, want %q", sidecar.LastExportHead, head)
	}
}

// TestPrepareExportNamesItsPredecessors: every snapshot names the heads it
// descends from, which is the whole basis of the chain-level fork proof.
func TestPrepareExportNamesItsPredecessors(t *testing.T) {
	transport, _ := openTransport(t)

	preds := []string{"1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"}
	batch, raw, err := transport.prepareExport(t.Context(), preds)
	if err != nil {
		t.Fatalf("prepareExport: %v", err)
	}
	if got := batch.Snapshot.Predecessors; len(got) != 2 || got[0] != preds[0] || got[1] != preds[1] {
		t.Fatalf("snapshot predecessors = %q, want %q", got, preds)
	}
	if len(raw) == 0 {
		t.Fatal("prepareExport produced no mirror bytes")
	}
}
