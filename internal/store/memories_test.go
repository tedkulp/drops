package store_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

func TestMemoryRoundTripsEveryField(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "gitops")

	want := newMemory(t, project, "gitops-r1w", "a lesson", "the body")
	want.Provenance = text("migrated from beads")
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID: want.ID, Kind: model.OwnerMemory, CreationReplica: replica,
		}); err != nil {
			return err
		}
		return tx.PutMemory(ctx, want)
	})

	got, err := opened.Memory(t.Context(), want.ID)
	if err != nil {
		t.Fatalf("read Memory: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Memory round trip:\n got %+v\nwant %+v", got, want)
	}
}

// TestSupersededMemoriesStayAddressable pins the retained notebook model: the
// old record keeps its identity and drops out of listings, and nothing else.
func TestSupersededMemoriesStayAddressable(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")

	retired := seedMemory(t, opened, project, "br-eyp", "the old lesson", "old body")
	replacement := seedMemory(t, opened, project, "br-et8", "the new lesson", "new body")

	linked := retired
	linked.SupersededBy = &replacement.ID
	linked.Revision, _ = retired.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateMemory(ctx, linked, retired.Revision)
	})

	current, err := opened.Memories(t.Context(), store.MemoryFilter{})
	if err != nil {
		t.Fatalf("list Memories: %v", err)
	}
	if ids := memoryIDs(current); !slices.Equal(ids, []model.ID{replacement.ID}) {
		t.Errorf("current Memories = %v, want just %s", ids, replacement.ID)
	}

	stored, err := opened.Memory(t.Context(), retired.ID)
	if err != nil {
		t.Fatalf("read the superseded Memory: %v", err)
	}
	if stored.SupersededBy == nil || *stored.SupersededBy != replacement.ID {
		t.Errorf("superseded_by = %v, want %s", stored.SupersededBy, replacement.ID)
	}

	withHistory, err := opened.Memories(t.Context(), store.MemoryFilter{IncludeSuperseded: true})
	if err != nil {
		t.Fatalf("list Memories: %v", err)
	}
	if len(withHistory) != 2 {
		t.Errorf("IncludeSuperseded listing = %d Memories, want 2", len(withHistory))
	}
}

// TestSupersessionStaysInsideOneProject pins the schema's composite reference:
// a Memory cannot be replaced by one owned by another Project.
func TestSupersessionStaysInsideOneProject(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	drops := seedProject(t, opened, replica, "drops")
	beacon := seedProject(t, opened, replica, "beacon")

	here := seedMemory(t, opened, drops, "br-eyp", "here", "body")
	elsewhere := seedMemory(t, opened, beacon, "beacon-1b0", "elsewhere", "body")

	linked := here
	linked.SupersededBy = &elsewhere.ID
	linked.Revision, _ = here.Revision.Next(replica)
	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateMemory(ctx, linked, here.Revision)
	})
	if !errors.Is(err, model.ErrNotFound) {
		t.Errorf("cross-Project supersession: err = %v, want ErrNotFound", err)
	}
}

func TestMemoriesAreScopedByProject(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	drops := seedProject(t, opened, replica, "drops")
	global := seedProject(t, opened, replica, model.GlobalProjectSlug)

	scoped := seedMemory(t, opened, drops, "br-et8", "a drops lesson", "body")
	shared := seedMemory(t, opened, global, "br-9nf", "a cross-project lesson", "body")

	listed, err := opened.Memories(t.Context(), store.MemoryFilter{Project: &drops.Key})
	if err != nil {
		t.Fatalf("list Memories: %v", err)
	}
	if ids := memoryIDs(listed); !slices.Equal(ids, []model.ID{scoped.ID}) {
		t.Errorf("Memories in drops = %v, want just %s", ids, scoped.ID)
	}

	all, err := opened.Memories(t.Context(), store.MemoryFilter{})
	if err != nil {
		t.Fatalf("list Memories: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("store-wide listing = %d Memories, want 2 (including %s)", len(all), shared.ID)
	}
}

func TestPutMemoryRefusesAnEmptyBody(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")
	empty := newMemory(t, project, "br-x", "a title", "")

	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutMemory(ctx, empty)
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Errorf("PutMemory with an empty body: err = %v, want ErrInvalid", err)
	}
}

func memoryIDs(memories []model.Memory) []model.ID {
	found := make([]model.ID, 0, len(memories))
	for _, memory := range memories {
		found = append(found, memory.ID)
	}
	return found
}
