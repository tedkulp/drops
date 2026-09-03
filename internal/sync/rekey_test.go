package sync

import (
	"errors"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// TestRekeyTakesTheStoreLock: rotating identity while another process is
// mid-sync would let that sync finish under a key the sidecar no longer names.
func TestRekeyTakesTheStoreLock(t *testing.T) {
	transport, _ := openTransportWith(t, Options{Locker: &heldLocker{}})
	if _, err := MintSidecar(transport.dir); err != nil {
		t.Fatal(err)
	}

	if _, err := transport.Rekey(t.Context()); !errors.Is(err, ErrSyncLocked) {
		t.Fatalf("Rekey against a held lock = %v, want ErrSyncLocked", err)
	}
}

// TestRekeyLeavesWrittenHistoryAlone: rekey rotates only the active key. It
// never rewrites a creation origin or an existing revision, because doing so
// would manufacture a merge winner out of records that have already been
// exported under the old key.
func TestRekeyLeavesWrittenHistoryAlone(t *testing.T) {
	transport, _ := openTransport(t)
	sidecar, err := MintSidecar(transport.dir)
	if err != nil {
		t.Fatal(err)
	}

	project, err := transport.core.CreateProject(t.Context(), "beacon")
	if err != nil {
		t.Fatal(err)
	}
	created, err := transport.core.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "written before the rekey", Type: model.TypeTask,
	})
	if err != nil {
		t.Fatal(err)
	}

	next, err := transport.Rekey(t.Context())
	if err != nil {
		t.Fatalf("Rekey: %v", err)
	}
	if next == sidecar.Replica {
		t.Fatalf("Rekey returned the old key %q", next)
	}
	loaded, err := LoadSidecar(transport.dir)
	if err != nil || loaded == nil {
		t.Fatalf("LoadSidecar = (%v, %v)", loaded, err)
	}
	if loaded.Replica != next {
		t.Fatalf("sidecar key = %q, want the rotated key %q", loaded.Replica, next)
	}

	after, err := transport.store.Issue(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CreationReplica != created.CreationReplica {
		t.Errorf("creation replica = %q, want the untouched %q", after.CreationReplica, created.CreationReplica)
	}
	if after.Revision != created.Revision {
		t.Errorf("revision = %+v, want the untouched %+v", after.Revision, created.Revision)
	}

	successions, err := transport.core.ReplicaSuccessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, succession := range successions {
		if succession.OldReplica == sidecar.Replica && succession.NewReplica == next {
			found = true
		}
	}
	if !found {
		t.Fatalf("successions %+v do not record %s -> %s", successions, sidecar.Replica, next)
	}
}

// TestRekeyRefusesAMalformedSidecar: an unreadable sidecar is a hard error
// everywhere, and rekey is not a way to launder one into a fresh identity.
func TestRekeyRefusesAMalformedSidecar(t *testing.T) {
	transport, _ := openTransport(t)
	if err := writeFileAtomic(transport.dir, sidecarFileName, []byte("{\"replica_key\":\"nope\"}\n")); err != nil {
		t.Fatal(err)
	}

	key, err := transport.Rekey(t.Context())
	if err == nil {
		t.Fatalf("Rekey over a malformed sidecar = (%q, nil), want a hard error", key)
	}
	// Not ErrNoActiveKey: an unreadable sidecar is a present one, and reporting
	// it as absent is how a fresh identity gets minted over a real key.
	if errors.Is(err, ErrNoActiveKey) {
		t.Fatalf("Rekey over a malformed sidecar = %v, want the sidecar error, not the missing-key one", err)
	}
}
