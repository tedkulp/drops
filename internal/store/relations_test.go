package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

func TestIssueParentIsTheOnlyAuthorityOnParentage(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	parent := seedIssue(t, opened, project, "dw32p", "the map")
	// A dotted ID spells its parent, and an unrelated ID does not. Neither
	// spelling is consulted: only the record decides.
	dotted := seedIssue(t, opened, project, "dw32p.19", "a dotted child")
	plain := seedIssue(t, opened, project, "k3f9x", "a plain child")

	revision, _ := model.InitialRevision(replica)
	record := model.IssueParent{
		ChildID:   plain.ID,
		ParentID:  parent.ID,
		CreatedAt: stampCreated,
		Revision:  revision,
	}
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutIssueParent(ctx, record)
	})

	got, err := opened.IssueParent(t.Context(), plain.ID)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if !reflect.DeepEqual(got, record) {
		t.Errorf("parent round trip:\n got %+v\nwant %+v", got, record)
	}

	if _, err := opened.IssueParent(t.Context(), dotted.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("parent of a dotted ID with no record: err = %v, want ErrNotFound", err)
	}

	children, err := opened.IssueChildren(t.Context(), parent.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 1 || children[0].ChildID != plain.ID {
		t.Errorf("children of %s = %+v, want just %s", parent.ID, children, plain.ID)
	}
}

func TestIssueParentRefusesSelfParentageAndUnknownEndpoints(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	issue := seedIssue(t, opened, project, "only", "the only Issue")
	revision, _ := model.InitialRevision(replica)

	itself := model.IssueParent{
		ChildID: issue.ID, ParentID: issue.ID, CreatedAt: stampCreated, Revision: revision,
	}
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutIssueParent(ctx, itself)
	}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("Issue parenting itself: err = %v, want ErrInvalid", err)
	}

	orphan := model.IssueParent{
		ChildID: issue.ID, ParentID: "absent", CreatedAt: stampCreated, Revision: revision,
	}
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutIssueParent(ctx, orphan)
	}); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("parent naming an absent Issue: err = %v, want ErrNotFound", err)
	}
}

func TestAChildHasAtMostOneParentRecord(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	child := seedIssue(t, opened, project, "child", "child")
	first := seedIssue(t, opened, project, "first", "first parent")
	second := seedIssue(t, opened, project, "second", "second parent")
	revision, _ := model.InitialRevision(replica)

	record := model.IssueParent{
		ChildID: child.ID, ParentID: first.ID, CreatedAt: stampCreated, Revision: revision,
	}
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutIssueParent(ctx, record)
	})

	rival := record
	rival.ParentID = second.ID
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutIssueParent(ctx, rival)
	}); !errors.Is(err, model.ErrConflict) {
		t.Errorf("a second parent record for one child: err = %v, want ErrConflict", err)
	}

	// Reparenting is an update of the one record, not a second row.
	moved := record
	moved.ParentID = second.ID
	moved.Revision, _ = record.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateIssueParent(ctx, moved, record.Revision)
	})
	got, err := opened.IssueParent(t.Context(), child.ID)
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if got.ParentID != second.ID {
		t.Errorf("parent after reparenting = %s, want %s", got.ParentID, second.ID)
	}
}

func TestDependenciesCarryEveryTypeAndDirection(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	blocked := seedIssue(t, opened, project, "blocked", "waits")
	blocker := seedIssue(t, opened, project, "blocker", "must land first")
	revision, _ := model.InitialRevision(replica)

	for _, kind := range []model.DependencyType{model.DepBlocks, model.DepRelated, model.DepDiscoveredFrom} {
		record := model.Dependency{
			FromID: blocked.ID, ToID: blocker.ID, Type: kind,
			CreatedAt: stampCreated, Revision: revision,
		}
		write(t, opened, func(ctx context.Context, tx *store.Tx) error {
			return tx.PutDependency(ctx, record)
		})
		got, err := opened.Dependency(t.Context(), blocked.ID, blocker.ID, kind)
		if err != nil {
			t.Fatalf("read %s dependency: %v", kind, err)
		}
		if !reflect.DeepEqual(got, record) {
			t.Errorf("%s dependency round trip:\n got %+v\nwant %+v", kind, got, record)
		}
	}

	from, err := opened.DependenciesFrom(t.Context(), blocked.ID)
	if err != nil {
		t.Fatalf("list dependencies from: %v", err)
	}
	if len(from) != 3 {
		t.Errorf("dependencies from %s = %d, want 3", blocked.ID, len(from))
	}
	to, err := opened.DependenciesTo(t.Context(), blocker.ID)
	if err != nil {
		t.Fatalf("list dependencies to: %v", err)
	}
	if len(to) != 3 {
		t.Errorf("dependencies to %s = %d, want 3", blocker.ID, len(to))
	}
}

// TestParentChildIsNotADependencyType pins that parentage left the dependency
// vocabulary: the v6 spelling is refused rather than quietly stored.
func TestParentChildIsNotADependencyType(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	child := seedIssue(t, opened, project, "child", "child")
	parent := seedIssue(t, opened, project, "parent", "parent")
	revision, _ := model.InitialRevision(replica)

	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutDependency(ctx, model.Dependency{
			FromID: child.ID, ToID: parent.ID, Type: "parent-child",
			CreatedAt: stampCreated, Revision: revision,
		})
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Errorf("parent-child dependency: err = %v, want ErrInvalid", err)
	}
}

func TestRemovingALabelTombstonesItRatherThanLosingIt(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	issue := seedIssue(t, opened, project, "labelled", "carries labels")
	revision, _ := model.InitialRevision(replica)

	label := model.Label{IssueID: issue.ID, Name: "wayfinder:map", Revision: revision}
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutLabel(ctx, label)
	})

	removed := label
	removed.Tombstone = model.Tombstoned
	removed.Revision, _ = label.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateLabel(ctx, removed, label.Revision)
	})

	live, err := opened.IssueLabels(t.Context(), issue.ID)
	if err != nil {
		t.Fatalf("list labels: %v", err)
	}
	if len(live) != 0 {
		t.Errorf("live labels = %+v, want none", live)
	}
	stored, err := opened.Label(t.Context(), issue.ID, label.Name)
	if err != nil {
		t.Fatalf("read the removed label: %v", err)
	}
	if stored.Tombstone != model.Tombstoned {
		t.Error("removed label is not tombstoned")
	}
	if stored.Revision.Generation != 2 {
		t.Errorf("removed label generation = %d, want 2", stored.Revision.Generation)
	}
}

func TestRelationWritesAllBumpTheWriteSequence(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	first := seedIssue(t, opened, project, "first", "first")
	second := seedIssue(t, opened, project, "second", "second")
	revision, _ := model.InitialRevision(replica)

	before := stateOf(t, opened).WriteSeq
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIssueParent(ctx, model.IssueParent{
			ChildID: second.ID, ParentID: first.ID, CreatedAt: stampCreated, Revision: revision,
		}); err != nil {
			return err
		}
		if err := tx.PutDependency(ctx, model.Dependency{
			FromID: first.ID, ToID: second.ID, Type: model.DepBlocks,
			CreatedAt: stampCreated, Revision: revision,
		}); err != nil {
			return err
		}
		return tx.PutLabel(ctx, model.Label{IssueID: first.ID, Name: "urgent", Revision: revision})
	})

	if got := stateOf(t, opened).WriteSeq - before; got != 3 {
		t.Errorf("write_seq advanced by %d across three relation writes, want 3", got)
	}
}
