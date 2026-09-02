package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

func TestCommentsAreAnAppendOnlyThread(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	issue := seedIssue(t, opened, project, "dw32p.19", "build the store")

	first := model.Comment{
		ID: "dw32p.19:0a1b2c3d4e5f", IssueID: issue.ID, Author: "ted",
		Body: "the first note", CreatedAt: "2026-08-30T10:00:00Z", CreationReplica: replica,
	}
	second := model.Comment{
		ID: "dw32p.19:112233445566", IssueID: issue.ID, Author: "claude-code/opus-5",
		Body: "the second note", CreatedAt: "2026-08-31T10:00:00Z", CreationReplica: replica,
	}
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutComment(ctx, second); err != nil {
			return err
		}
		return tx.PutComment(ctx, first)
	})

	thread, err := opened.IssueComments(t.Context(), issue.ID)
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	if !reflect.DeepEqual(thread, []model.Comment{first, second}) {
		t.Errorf("thread = %+v, want oldest first", thread)
	}

	// A Comment's mirror revision is derived, never stored.
	if revision := first.Revision(); revision.Generation != 1 || revision.Replica != replica {
		t.Errorf("derived Comment revision = %+v, want generation 1 under %s", revision, replica)
	}

	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutComment(ctx, first)
	}); !errors.Is(err, model.ErrConflict) {
		t.Errorf("re-adding a Comment: err = %v, want ErrConflict", err)
	}
}

func TestDeleteCommentRetractsOne(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	issue := seedIssue(t, opened, project, "k3f9x", "an Issue")

	comment := model.Comment{
		ID: "k3f9x:aabbccddeeff", IssueID: issue.ID, Body: "a mistake",
		CreatedAt: "2026-08-30T10:00:00Z", CreationReplica: replica,
	}
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutComment(ctx, comment)
	})
	before := stateOf(t, opened).WriteSeq

	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.DeleteComment(ctx, comment.ID)
	})
	if _, err := opened.Comment(t.Context(), comment.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("read after delete: err = %v, want ErrNotFound", err)
	}
	if got := stateOf(t, opened).WriteSeq - before; got != 1 {
		t.Errorf("write_seq advanced by %d on a delete, want 1", got)
	}

	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.DeleteComment(ctx, comment.ID)
	}); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("deleting twice: err = %v, want ErrNotFound", err)
	}
}

func TestRepositoryLocatorsAreEvidenceNotIdentity(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	first := seedProject(t, opened, replica, "drops")
	second := seedProject(t, opened, replica, "drops-fork")
	revision, _ := model.InitialRevision(replica)

	const shared = "github.com/tedkulp/drops"
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutRepositoryLocator(ctx, model.RepositoryLocator{
			ProjectKey: first.Key, Locator: shared, Revision: revision,
		}); err != nil {
			return err
		}
		return tx.PutRepositoryLocator(ctx, model.RepositoryLocator{
			ProjectKey: second.Key, Locator: shared, Revision: revision,
		})
	})

	candidates, err := opened.ProjectsByLocator(t.Context(), shared)
	if err != nil {
		t.Fatalf("look up by locator: %v", err)
	}
	if len(candidates) != 2 {
		t.Errorf("locator %q names %d Projects, want 2: choosing between them is not this layer's call",
			shared, len(candidates))
	}
}

// TestLocalRecordsDoNotMarkTheStoreDirty pins the split the mirror depends on: a
// machine's own bindings and transport bookkeeping are not replicated state.
func TestLocalRecordsDoNotMarkTheStoreDirty(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	before := stateOf(t, opened).WriteSeq

	successor := newReplicaKey(t)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutWorkspaceBinding(ctx, model.WorkspaceBinding{
			Path: "/home/ted/src/drops", ProjectKey: project.Key,
		}); err != nil {
			return err
		}
		if err := tx.PutReplicaHead(ctx, model.ReplicaHead{
			Replica: replica, SnapshotHead: "9f3c1a",
		}); err != nil {
			return err
		}
		return tx.PutReplicaSuccession(ctx, model.ReplicaSuccession{
			OldReplica: replica, NewReplica: successor, RekeyedAt: stampUpdated,
		})
	})

	if got := stateOf(t, opened).WriteSeq; got != before {
		t.Errorf("write_seq = %d after local writes, want %d unchanged", got, before)
	}
}

func TestWorkspaceBindingsRebindAndUnbind(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	first := seedProject(t, opened, replica, "drops")
	second := seedProject(t, opened, replica, "beacon")
	const path = "/home/ted/src/drops"

	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutWorkspaceBinding(ctx, model.WorkspaceBinding{Path: path, ProjectKey: first.Key})
	})
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutWorkspaceBinding(ctx, model.WorkspaceBinding{Path: path, ProjectKey: second.Key})
	})

	binding, err := opened.WorkspaceBinding(t.Context(), path)
	if err != nil {
		t.Fatalf("read binding: %v", err)
	}
	if binding.ProjectKey != second.Key {
		t.Errorf("binding names %s, want the rebound %s", binding.ProjectKey, second.Key)
	}

	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.DeleteWorkspaceBinding(ctx, path)
	})
	if _, err := opened.WorkspaceBinding(t.Context(), path); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("read after unbind: err = %v, want ErrNotFound", err)
	}
}

func TestReplicaHeadsAdvanceAndSuccessionIsOneToOne(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)

	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutReplicaHead(ctx, model.ReplicaHead{Replica: replica, SnapshotHead: "first"})
	})
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutReplicaHead(ctx, model.ReplicaHead{Replica: replica, SnapshotHead: "second"})
	})
	head, err := opened.ReplicaHead(t.Context(), replica)
	if err != nil {
		t.Fatalf("read replica head: %v", err)
	}
	if head.SnapshotHead != "second" {
		t.Errorf("head = %q, want the advanced %q", head.SnapshotHead, "second")
	}

	successor := newReplicaKey(t)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutReplicaSuccession(ctx, model.ReplicaSuccession{
			OldReplica: replica, NewReplica: successor, RekeyedAt: stampUpdated,
		})
	})
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutReplicaSuccession(ctx, model.ReplicaSuccession{
			OldReplica: newReplicaKey(t), NewReplica: successor, RekeyedAt: stampUpdated,
		})
	}); !errors.Is(err, model.ErrConflict) {
		t.Errorf("two keys succeeded by one: err = %v, want ErrConflict", err)
	}

	successions, err := opened.ReplicaSuccessions(t.Context())
	if err != nil {
		t.Fatalf("list successions: %v", err)
	}
	if len(successions) != 1 || successions[0].NewReplica != successor {
		t.Errorf("successions = %+v, want one naming %s", successions, successor)
	}
}
