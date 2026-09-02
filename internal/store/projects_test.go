package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

func TestProjectRoundTripsEveryField(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)

	archived := newProject(t, replica, "retired")
	archived.ArchivedAt = stamp("2026-01-02T03:04:05Z")
	live := newProject(t, replica, "beacon")

	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutProject(ctx, archived); err != nil {
			return err
		}
		return tx.PutProject(ctx, live)
	})

	for _, want := range []model.Project{archived, live} {
		got, err := opened.Project(t.Context(), want.Key)
		if err != nil {
			t.Fatalf("read Project %s: %v", want.Key, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Project %s round trip:\n got %+v\nwant %+v", want.Key, got, want)
		}
		bySlug, err := opened.ProjectBySlug(t.Context(), want.Slug)
		if err != nil {
			t.Fatalf("read Project %q by slug: %v", want.Slug, err)
		}
		if bySlug.Key != want.Key {
			t.Errorf("ProjectBySlug(%q).Key = %s, want %s", want.Slug, bySlug.Key, want.Key)
		}
	}
}

func TestProjectReadsReportNotFound(t *testing.T) {
	opened := newStore(t)

	if _, err := opened.Project(t.Context(), newProjectKey(t)); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("Project(absent): err = %v, want ErrNotFound", err)
	}
	if _, err := opened.ProjectBySlug(t.Context(), "absent"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("ProjectBySlug(absent): err = %v, want ErrNotFound", err)
	}
}

func TestEveryReplicatedWriteBumpsTheWriteSequence(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)

	if seq := stateOf(t, opened).WriteSeq; seq != 0 {
		t.Fatalf("fresh store write_seq = %d, want 0", seq)
	}

	first := seedProject(t, opened, replica, "one")
	if seq := stateOf(t, opened).WriteSeq; seq != 1 {
		t.Errorf("write_seq after one insert = %d, want 1", seq)
	}

	seedProject(t, opened, replica, "two")
	if seq := stateOf(t, opened).WriteSeq; seq != 2 {
		t.Errorf("write_seq after two inserts = %d, want 2", seq)
	}

	next, err := first.Revision.Next(replica)
	if err != nil {
		t.Fatalf("advance revision: %v", err)
	}
	renamed := first
	renamed.Slug = "renamed"
	renamed.Revision = next
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateProject(ctx, renamed, first.Revision)
	})
	if seq := stateOf(t, opened).WriteSeq; seq != 3 {
		t.Errorf("write_seq after an update = %d, want 3", seq)
	}
}

func TestMarkExportedIsNotAReplicatedWrite(t *testing.T) {
	opened := newStore(t)
	seedProject(t, opened, newReplicaKey(t), "one")

	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.MarkExported(ctx, 1, "9f3c1a")
	})

	state := stateOf(t, opened)
	if state.WriteSeq != 1 {
		t.Errorf("write_seq after MarkExported = %d, want 1", state.WriteSeq)
	}
	if state.ExportedWriteSeq != 1 || state.LastExportHead != "9f3c1a" {
		t.Errorf("state after MarkExported = %+v, want exported 1 at 9f3c1a", state)
	}
	if !state.Exported() {
		t.Error("Exported() = false with every write exported")
	}
}

func TestARolledBackTransactionLeavesNoTrace(t *testing.T) {
	opened := newStore(t)
	project := newProject(t, newReplicaKey(t), "doomed")

	wantErr := errors.New("caller changed its mind")
	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutProject(ctx, project); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithTx error = %v, want the caller's error", err)
	}

	if _, err := opened.Project(t.Context(), project.Key); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("Project after rollback: err = %v, want ErrNotFound", err)
	}
	if seq := stateOf(t, opened).WriteSeq; seq != 0 {
		t.Errorf("write_seq after rollback = %d, want 0", seq)
	}
}

func TestPutProjectRefusesATakenKeyOrSlug(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	existing := seedProject(t, opened, replica, "beacon")

	sameKey := newProject(t, replica, "different-slug")
	sameKey.Key = existing.Key
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutProject(ctx, sameKey)
	}); !errors.Is(err, model.ErrConflict) {
		t.Errorf("PutProject with a taken key: err = %v, want ErrConflict", err)
	}

	sameSlug := newProject(t, replica, existing.Slug)
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutProject(ctx, sameSlug)
	}); !errors.Is(err, model.ErrConflict) {
		t.Errorf("PutProject with a taken slug: err = %v, want ErrConflict", err)
	}
}

func TestPutProjectRefusesAnInvalidRecord(t *testing.T) {
	opened := newStore(t)

	// An empty slug is refused by the schema too; a malformed Project key and a
	// malformed timestamp are refused only by the record's own invariants, so
	// they are what proves the write validates before it reaches SQLite.
	cases := map[string]func(*model.Project){
		"empty slug":        func(project *model.Project) { project.Slug = "" },
		"malformed key":     func(project *model.Project) { project.Key = "not-a-project-key" },
		"malformed created": func(project *model.Project) { project.CreatedAt = "yesterday" },
		"zero generation":   func(project *model.Project) { project.Revision.Generation = 0 },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			invalid := newProject(t, newReplicaKey(t), "valid")
			breakIt(&invalid)
			err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
				return tx.PutProject(ctx, invalid)
			})
			if !errors.Is(err, model.ErrInvalid) {
				t.Errorf("PutProject with a %s: err = %v, want ErrInvalid", name, err)
			}
		})
	}
}

func TestUpdateProjectIsACompareAndSwap(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "beacon")

	winner := project
	winner.Slug = "beacon-renamed"
	winner.Revision, _ = project.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateProject(ctx, winner, project.Revision)
	})

	loser := project
	loser.Slug = "beacon-also-renamed"
	loser.Revision, _ = project.Revision.Next(replica)
	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateProject(ctx, loser, project.Revision)
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second update from a stale revision: err = %v, want ErrConflict", err)
	}

	stored, err := opened.Project(t.Context(), project.Key)
	if err != nil {
		t.Fatalf("read Project: %v", err)
	}
	if stored.Slug != winner.Slug {
		t.Errorf("stored slug = %q, want the first writer's %q", stored.Slug, winner.Slug)
	}
	if seq := stateOf(t, opened).WriteSeq; seq != 2 {
		t.Errorf("write_seq = %d, want 2: a refused update must not count as a write", seq)
	}
}

func TestUpdateProjectReportsAMissingProject(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	absent := newProject(t, replica, "nowhere")

	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateProject(ctx, absent, absent.Revision)
	})
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("UpdateProject on an absent Project: err = %v, want ErrNotFound", err)
	}
}

func TestUpdateProjectLeavesCreationOriginUnchanged(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "beacon")

	other := newReplicaKey(t)
	edited := project
	edited.CreationReplica = other
	edited.Revision, _ = project.Revision.Next(other)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateProject(ctx, edited, project.Revision)
	})

	stored, err := opened.Project(t.Context(), project.Key)
	if err != nil {
		t.Fatalf("read Project: %v", err)
	}
	if stored.CreationReplica != replica {
		t.Errorf("creation replica = %s, want the original %s", stored.CreationReplica, replica)
	}
	if stored.Revision != edited.Revision {
		t.Errorf("revision = %+v, want %+v", stored.Revision, edited.Revision)
	}
}

func TestProjectsListsInKeyOrder(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	seeded := []model.Project{
		seedProject(t, opened, replica, "alpha"),
		seedProject(t, opened, replica, "beta"),
		seedProject(t, opened, replica, "gamma"),
	}

	listed, err := opened.Projects(t.Context())
	if err != nil {
		t.Fatalf("list Projects: %v", err)
	}
	if len(listed) != len(seeded) {
		t.Fatalf("listed %d Projects, want %d", len(listed), len(seeded))
	}
	for index := 1; index < len(listed); index++ {
		if listed[index-1].Key >= listed[index].Key {
			t.Errorf("Projects not in key order: %s then %s", listed[index-1].Key, listed[index].Key)
		}
	}
}
