package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// issuesNamed opens a Core whose ids are pinned and creates one Issue per id,
// so a relation test can name both ends of the edge it is about. It is
// blockingGraph with no edges: the wiring is what these tests are about.
func issuesNamed(t *testing.T, ids ...model.ID) (*core.Core, *store.Store) {
	t.Helper()
	rules, opened := openCoreWithIDs(t, ids...)
	blockingGraph(t, rules, ids, nil)
	return rules, opened
}

// liveEdges reads back the stored rows of one type, tombstones excluded, as the
// spellings they are stored under. The store is asked directly because the
// whole question here is how many ROWS one relation costs.
func liveEdges(t *testing.T, opened *store.Store, kind model.DependencyType) [][2]model.ID {
	t.Helper()
	dependencies, err := opened.Dependencies(t.Context())
	if err != nil {
		t.Fatalf("list dependencies: %v", err)
	}
	out := [][2]model.ID{}
	for _, dependency := range dependencies {
		if dependency.Type == kind && dependency.Tombstone == model.Live {
			out = append(out, [2]model.ID{dependency.FromID, dependency.ToID})
		}
	}
	return out
}

// The clause: an undirected edge is ONE relation however it is spelled, so
// adding the reciprocal `related` edge is the no-op adding it twice already
// was. Two rows for one relation is what made a reciprocal pair render and
// serialize twice (y7f6z).
func TestAddingTheReciprocalRelatedEdgeIsTheSameEdge(t *testing.T) {
	rules, opened := issuesNamed(t, "aaa", "bbb")

	first, err := rules.SetDependency(t.Context(), "aaa", "bbb", model.DepRelated, true)
	if err != nil {
		t.Fatalf("add related: %v", err)
	}
	again, err := rules.SetDependency(t.Context(), "bbb", "aaa", model.DepRelated, true)
	if err != nil {
		t.Fatalf("add the reciprocal related edge: %v", err)
	}
	if again.FromID != first.FromID || again.ToID != first.ToID {
		t.Errorf("the reciprocal spelling stored %s -> %s, want the one edge %s -> %s",
			again.FromID, again.ToID, first.FromID, first.ToID)
	}
	if again.Revision != first.Revision {
		t.Errorf("revision = %#v, want the untouched %#v: a no-op advances nothing",
			again.Revision, first.Revision)
	}
	if got := liveEdges(t, opened, model.DepRelated); len(got) != 1 {
		t.Fatalf("live related rows = %v, want the one edge", got)
	}
	view, err := rules.ViewIssue(t.Context(), "aaa")
	if err != nil {
		t.Fatalf("view Issue: %v", err)
	}
	if got := refIDs(core.RelatedRefs(view)); len(got) != 1 || got[0] != "bbb" {
		t.Fatalf("related = %v, want [bbb] once", got)
	}
}

// The clause: removal of an undirected edge withdraws whichever direction the
// store holds, so `dep rm` spelled from the other end is not a not-found
// refusal beside a relation that is still there.
func TestRemovingAnUndirectedEdgeWithdrawsWhicheverDirectionIsStored(t *testing.T) {
	rules, opened := issuesNamed(t, "aaa", "bbb")
	if _, err := rules.SetDependency(t.Context(), "aaa", "bbb", model.DepRelated, true); err != nil {
		t.Fatalf("add related: %v", err)
	}

	removed, err := rules.SetDependency(t.Context(), "bbb", "aaa", model.DepRelated, false)
	if err != nil {
		t.Fatalf("remove the reciprocal spelling: %v", err)
	}
	if removed.Tombstone != model.Tombstoned {
		t.Errorf("removed edge = %#v, want tombstoned", removed)
	}
	if got := liveEdges(t, opened, model.DepRelated); len(got) != 0 {
		t.Fatalf("live related rows = %v, want none", got)
	}
}

// The clause: the reader is not the only end that has to survive a reciprocal
// pair. Two replicas each adding one direction is how the pair arrives with
// nobody spelling it twice, and removal is then a repair: it withdraws BOTH
// halves, because they are one relation to every reader.
func TestRemovingAnUndirectedEdgeWithdrawsBothHalvesOfASyncedPair(t *testing.T) {
	rules, opened := issuesNamed(t, "aaa", "bbb")
	putReciprocalPair(t, opened, "aaa", "bbb")

	if _, err := rules.SetDependency(t.Context(), "aaa", "bbb", model.DepRelated, false); err != nil {
		t.Fatalf("remove related: %v", err)
	}
	if got := liveEdges(t, opened, model.DepRelated); len(got) != 0 {
		t.Fatalf("live related rows = %v, want none: half a removed relation still renders", got)
	}
}

// The clause: adding an undirected edge never leaves TWO live rows for one
// relation. A tombstoned row beside a live reciprocal is sync's doing — one
// replica removed the edge while the other added the reverse spelling — and
// reviving the tombstone would spell the live relation twice.
func TestAddingAnUndirectedEdgeNeverRevivesASecondLiveRow(t *testing.T) {
	rules, opened := issuesNamed(t, "aaa", "bbb")
	putRelatedEdge(t, opened, "aaa", "bbb", "aaaaaaaaaaaaaaaaaaaaaaaaae", model.Tombstoned)
	putRelatedEdge(t, opened, "bbb", "aaa", "bbbbbbbbbbbbbbbbbbbbbbbbbe", model.Live)

	changed, err := rules.SetDependency(t.Context(), "aaa", "bbb", model.DepRelated, true)
	if err != nil {
		t.Fatalf("add related: %v", err)
	}
	if changed.FromID != "bbb" || changed.ToID != "aaa" || changed.Tombstone != model.Live {
		t.Errorf("add returned %s -> %s (%v), want the live bbb -> aaa it already had",
			changed.FromID, changed.ToID, changed.Tombstone)
	}
	got := liveEdges(t, opened, model.DepRelated)
	if len(got) != 1 || got[0] != [2]model.ID{"bbb", "aaa"} {
		t.Fatalf("live related rows = %v, want only the bbb -> aaa the store already held", got)
	}
}

// The clause: `blocks` and `discovered-from` are DIRECTED, so the reciprocal
// spelling is a second, different edge — A blocks B and B blocks A is a cycle
// `dep cycles` is there to find, not a duplicate to collapse.
func TestTheReciprocalSpellingOfADirectedEdgeIsADifferentEdge(t *testing.T) {
	rules, opened := issuesNamed(t, "aaa", "bbb")
	for _, edge := range [][2]model.ID{{"aaa", "bbb"}, {"bbb", "aaa"}} {
		if _, err := rules.SetDependency(t.Context(), edge[0], edge[1], model.DepBlocks, true); err != nil {
			t.Fatalf("block %s on %s: %v", edge[0], edge[1], err)
		}
	}
	if got := liveEdges(t, opened, model.DepBlocks); len(got) != 2 {
		t.Fatalf("live blocks rows = %v, want both directions", got)
	}

	if _, err := rules.SetDependency(t.Context(), "aaa", "bbb", model.DepBlocks, false); err != nil {
		t.Fatalf("remove blocks: %v", err)
	}
	got := liveEdges(t, opened, model.DepBlocks)
	if len(got) != 1 || got[0] != [2]model.ID{"bbb", "aaa"} {
		t.Fatalf("live blocks rows = %v, want the untouched bbb -> aaa", got)
	}
}

// The clause: removing an edge the store does not hold in EITHER direction is
// still a not-found refusal, which is what `dep rm`'s exit 4 is.
func TestRemovingAnAbsentUndirectedEdgeIsStillNotFound(t *testing.T) {
	rules, _ := issuesNamed(t, "aaa", "bbb")
	_, err := rules.SetDependency(t.Context(), "aaa", "bbb", model.DepRelated, false)
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("remove of an absent related edge = %v, want ErrNotFound", err)
	}
}

// putRelatedEdge writes one `related` row straight to the store under a named
// replica. It is how a test builds a state core refuses to write: sync merges
// records that share no primary key, so both spellings of one relation can
// arrive with nobody having spelled either twice (y7f6z).
func putRelatedEdge(t *testing.T, opened *store.Store, from, to model.ID, replicaKey string, tombstone model.Tombstone) {
	t.Helper()
	err := opened.WithTx(t.Context(), func(ctx context.Context, tx *store.Tx) error {
		replica, err := model.ParseReplicaKey(replicaKey)
		if err != nil {
			return err
		}
		revision, err := model.InitialRevision(replica)
		if err != nil {
			return err
		}
		return tx.PutDependency(ctx, model.Dependency{
			FromID: from, ToID: to, Type: model.DepRelated,
			CreatedAt: "2026-09-02T15:00:00Z", Tombstone: tombstone, Revision: revision,
		})
	})
	if err != nil {
		t.Fatalf("write %s related %s: %v", from, to, err)
	}
}

// putReciprocalPair writes both spellings of one live `related` edge, each
// authored by a different replica: two machines each related the pair from
// their own end, and sync merged both records.
func putReciprocalPair(t *testing.T, opened *store.Store, first, second model.ID) {
	t.Helper()
	putRelatedEdge(t, opened, first, second, "aaaaaaaaaaaaaaaaaaaaaaaaae", model.Live)
	putRelatedEdge(t, opened, second, first, "bbbbbbbbbbbbbbbbbbbbbbbbbe", model.Live)
}
