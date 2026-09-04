package core_test

import (
	"slices"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// openCoreWithIDs is openCore with the ID source pinned, so a test that reasons
// about ordering can name its Issues rather than discover them.
func openCoreWithIDs(t *testing.T, ids ...model.ID) (*core.Core, *store.Store) {
	t.Helper()
	_, opened := openCore(t)
	replica, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	clock := fixedClock{now: time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC)}
	return core.New(opened, replica, clock, &sequenceIDs{values: ids}, nil), opened
}

// blockingGraph creates one Issue per id and wires each "from blocked by to"
// pair, returning the Project the whole graph lives in.
func blockingGraph(t *testing.T, rules *core.Core, ids []model.ID, edges [][2]model.ID) model.ProjectKey {
	t.Helper()
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	for _, id := range ids {
		if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
			Project: project.Key, Title: string(id), Type: model.TypeTask, Priority: 2,
		}); err != nil {
			t.Fatalf("create Issue %s: %v", id, err)
		}
	}
	for _, edge := range edges {
		if _, err := rules.SetDependency(t.Context(), edge[0], edge[1], model.DepBlocks, true); err != nil {
			t.Fatalf("block %s on %s: %v", edge[0], edge[1], err)
		}
	}
	return project.Key
}

// The clause: LongestBlockerChain returns the DEEPEST simple path, not the
// first or the last one it walks. The shallow branch is deliberately the
// lower-sorting id, so an implementation that keeps whichever branch it saw
// first returns the wrong chain rather than a differently-ordered right one.
func TestLongestBlockerChainTakesTheDeepestPathNotTheFirst(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "root", "aaa", "mmm", "nnn", "ooo")
	blockingGraph(t,
		rules,
		[]model.ID{"root", "aaa", "mmm", "nnn", "ooo"},
		[][2]model.ID{{"root", "aaa"}, {"root", "mmm"}, {"mmm", "nnn"}, {"nnn", "ooo"}},
	)

	chain, err := rules.LongestBlockerChain(t.Context(), "root")
	if err != nil {
		t.Fatalf("longest chain: %v", err)
	}
	want := []model.ID{"root", "mmm", "nnn", "ooo"}
	if !slices.Equal(chain, want) {
		t.Fatalf("chain = %v, want %v", chain, want)
	}
}

// The clause: the chain is a SIMPLE path — a cycle terminates at the first
// repeated node rather than walking it twice or forever.
func TestLongestBlockerChainStopsAtTheFirstRepeatedNode(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "root", "mmm", "nnn")
	blockingGraph(t,
		rules,
		[]model.ID{"root", "mmm", "nnn"},
		[][2]model.ID{{"root", "mmm"}, {"mmm", "nnn"}, {"nnn", "root"}},
	)

	chain, err := rules.LongestBlockerChain(t.Context(), "root")
	if err != nil {
		t.Fatalf("longest chain: %v", err)
	}
	want := []model.ID{"root", "mmm", "nnn"}
	if !slices.Equal(chain, want) {
		t.Fatalf("chain = %v, want %v", chain, want)
	}
	seen := map[model.ID]bool{}
	for _, node := range chain {
		if seen[node] {
			t.Fatalf("chain %v repeats %s", chain, node)
		}
		seen[node] = true
	}
}

// A blocker chain is asked for by id, so an id that names no Issue is a read
// error rather than a one-element chain invented out of the missing node.
func TestLongestBlockerChainRefusesAnUnknownIssue(t *testing.T) {
	rules, _ := openCore(t)
	if _, err := rules.LongestBlockerChain(t.Context(), "nobody"); err == nil {
		t.Fatal("longest chain of an unknown Issue: err = nil, want not found")
	}
}

// The clause: a cycle is reported in canonical rotation — lowest id first —
// so the same cycle reached from a different entry point reads identically.
// The DFS enters this cycle at nnn (via the lower-sorting aaa, which is not
// itself in the cycle), so the raw stack slice is [nnn mmm] and only the
// rotation makes it [mmm nnn].
func TestDependencyCyclesRotateToTheLowestID(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "aaa", "mmm", "nnn")
	blockingGraph(t,
		rules,
		[]model.ID{"aaa", "mmm", "nnn"},
		[][2]model.ID{{"aaa", "nnn"}, {"nnn", "mmm"}, {"mmm", "nnn"}},
	)

	cycles, err := rules.DependencyCycles(t.Context())
	if err != nil {
		t.Fatalf("dependency cycles: %v", err)
	}
	if len(cycles) != 1 {
		t.Fatalf("cycles = %v, want exactly one", cycles)
	}
	want := []model.ID{"mmm", "nnn"}
	if !slices.Equal(cycles[0], want) {
		t.Fatalf("cycle = %v, want %v", cycles[0], want)
	}
}

// The clause: only LIVE BLOCKS edges form the cycle graph. Each of the two
// halves gets its own would-be cycle, so removing either half of the condition
// invents a cycle that is not there.
func TestDependencyCyclesIgnoreTombstonedAndNonBlockingEdges(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "aaa", "bbb", "ccc", "ddd")
	blockingGraph(t,
		rules,
		[]model.ID{"aaa", "bbb", "ccc", "ddd"},
		[][2]model.ID{{"aaa", "bbb"}, {"ccc", "ddd"}},
	)

	// aaa <-> bbb would cycle, but the returning edge is tombstoned.
	if _, err := rules.SetDependency(t.Context(), "bbb", "aaa", model.DepBlocks, true); err != nil {
		t.Fatalf("add returning blocks edge: %v", err)
	}
	if _, err := rules.SetDependency(t.Context(), "bbb", "aaa", model.DepBlocks, false); err != nil {
		t.Fatalf("tombstone returning blocks edge: %v", err)
	}
	// ccc <-> ddd would cycle, but the returning edge is not a block.
	if _, err := rules.SetDependency(t.Context(), "ddd", "ccc", model.DepRelated, true); err != nil {
		t.Fatalf("add returning related edge: %v", err)
	}

	cycles, err := rules.DependencyCycles(t.Context())
	if err != nil {
		t.Fatalf("dependency cycles: %v", err)
	}
	if len(cycles) != 0 {
		t.Fatalf("cycles = %v, want none", cycles)
	}
}

// The clause: UnblockImpacts counts, per named blocker, the open Issues that
// blocker ALONE holds back — an Issue with a second open blocker would not
// become ready, so it is not an impact.
func TestUnblockImpactsCountOnlySoleBlockers(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "hub", "solo", "shared", "other")
	blockingGraph(t,
		rules,
		[]model.ID{"hub", "solo", "shared", "other"},
		[][2]model.ID{{"solo", "hub"}, {"shared", "hub"}, {"shared", "other"}},
	)

	impacts, err := rules.UnblockImpacts(t.Context(), []model.ID{"hub", "other"})
	if err != nil {
		t.Fatalf("unblock impacts: %v", err)
	}
	if impacts["hub"] != 1 {
		t.Errorf("hub impact = %d, want 1 (shared is also blocked by other)", impacts["hub"])
	}
	if impacts["other"] != 0 {
		t.Errorf("other impact = %d, want 0", impacts["other"])
	}
}

// The clause: a deferred Issue is neither ready nor blocked until its deferral
// passes, and the boundary is strict — a deferral at exactly now has passed.
func TestDeferredIssuesLeaveBothReadyAndBlocked(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "later", "now")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	// openCoreWithIDs pins the clock to 15:00:00 UTC on 2026-09-02.
	future := model.NewTimestamp(time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC))
	present := model.NewTimestamp(time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC))
	for _, seed := range []struct {
		title string
		until model.Timestamp
	}{{"later", future}, {"now", present}} {
		until := seed.until
		if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
			Project: project.Key, Title: seed.title, Type: model.TypeTask, Priority: 2,
			DeferredUntil: &until,
		}); err != nil {
			t.Fatalf("create %s: %v", seed.title, err)
		}
	}

	ready, err := rules.Ready(t.Context(), core.IssueFilter{Project: &project.Key})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "now" {
		t.Fatalf("ready = %#v, want only the Issue whose deferral has passed", ready)
	}
}

// The clause: every distinct cycle is reported, in a deterministic order, so
// two runs over the same graph produce the same bytes.
func TestDependencyCyclesReportEveryCycleInDeterministicOrder(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "aaa", "bbb", "ccc", "ddd", "eee", "fff")
	blockingGraph(t,
		rules,
		[]model.ID{"aaa", "bbb", "ccc", "ddd", "eee", "fff"},
		[][2]model.ID{
			{"aaa", "bbb"}, {"bbb", "aaa"}, // one two-node cycle
			{"ccc", "ddd"}, {"ddd", "eee"}, {"eee", "ccc"}, // one three-node cycle
			{"aaa", "ccc"}, {"fff", "aaa"}, // edges into both, in neither
		},
	)

	cycles, err := rules.DependencyCycles(t.Context())
	if err != nil {
		t.Fatalf("dependency cycles: %v", err)
	}
	want := [][]model.ID{{"aaa", "bbb"}, {"ccc", "ddd", "eee"}}
	if len(cycles) != len(want) {
		t.Fatalf("cycles = %v, want %v", cycles, want)
	}
	for i := range want {
		if !slices.Equal(cycles[i], want[i]) {
			t.Fatalf("cycles = %v, want %v", cycles, want)
		}
	}
}

// fixedNow is the clock every core test reads, so a timestamp assertion is a
// literal rather than a re-derivation of what the code did.
var fixedNow = time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC)

// The clause: OpenBlockers reports the open-blocker map for the whole live
// graph, not for the candidate set Ready and Blocked scope themselves to. The
// left pane lists in_progress rows (qy3de.4), and Blocked's own default
// candidate set cannot see one, so the annotation has to come from here.
func TestOpenBlockersCoversTheRowsBlockedCannotSee(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "doing", "wall")
	project := blockingGraph(t, rules,
		[]model.ID{"doing", "wall"},
		[][2]model.ID{{"doing", "wall"}},
	)
	if _, err := rules.SetIssueStatus(t.Context(), "doing", model.StatusInProgress, ""); err != nil {
		t.Fatalf("start doing: %v", err)
	}

	blocked, err := rules.Blocked(t.Context(), core.IssueFilter{Project: &project})
	if err != nil {
		t.Fatalf("blocked: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("blocked = %#v, want none: an in_progress Issue is not an open candidate", blocked)
	}

	blockers, err := rules.OpenBlockers(t.Context())
	if err != nil {
		t.Fatalf("open blockers: %v", err)
	}
	if want := []model.ID{"wall"}; !slices.Equal(blockers["doing"], want) {
		t.Fatalf("blockers[doing] = %v, want %v", blockers["doing"], want)
	}
	if len(blockers) != 1 {
		t.Fatalf("blockers = %v, want only the blocked Issue keyed", blockers)
	}
}

// Two clauses, one graph. Ready and Blocked scope their candidates through
// filter.Statuses when the caller names one, rather than discarding it; and an
// unset Statuses still means open only, which is what keeps `drops ready` and
// `drops blocked` byte-identical (neither passes a status set).
func TestReadyAndBlockedScopeCandidatesByStatus(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "doing", "going", "wall")
	project := blockingGraph(t, rules,
		[]model.ID{"doing", "going", "wall"},
		[][2]model.ID{{"doing", "wall"}},
	)
	for _, id := range []model.ID{"doing", "going"} {
		if _, err := rules.SetIssueStatus(t.Context(), id, model.StatusInProgress, ""); err != nil {
			t.Fatalf("start %s: %v", id, err)
		}
	}

	t.Run("honours the set the caller names", func(t *testing.T) {
		filter := core.IssueFilter{Project: &project, Statuses: []model.Status{model.StatusInProgress}}

		ready, err := rules.Ready(t.Context(), filter)
		if err != nil {
			t.Fatalf("ready: %v", err)
		}
		if len(ready) != 1 || ready[0].ID != "going" {
			t.Errorf("ready = %v, want only going: the unblocked in_progress Issue", ids(ready))
		}

		blocked, err := rules.Blocked(t.Context(), filter)
		if err != nil {
			t.Fatalf("blocked: %v", err)
		}
		if len(blocked) != 1 || blocked[0].Issue.ID != "doing" {
			t.Fatalf("blocked = %#v, want only doing: the blocked in_progress Issue", blocked)
		}
		if want := []model.ID{"wall"}; !slices.Equal(blocked[0].BlockedBy, want) {
			t.Errorf("doing blocked by %v, want %v", blocked[0].BlockedBy, want)
		}
	})

	t.Run("defaults to open when the caller names none", func(t *testing.T) {
		filter := core.IssueFilter{Project: &project}

		ready, err := rules.Ready(t.Context(), filter)
		if err != nil {
			t.Fatalf("ready: %v", err)
		}
		if len(ready) != 1 || ready[0].ID != "wall" {
			t.Errorf("ready = %v, want only wall: the one open Issue", ids(ready))
		}

		blocked, err := rules.Blocked(t.Context(), filter)
		if err != nil {
			t.Fatalf("blocked: %v", err)
		}
		if len(blocked) != 0 {
			t.Errorf("blocked = %#v, want none: the only blocked Issue is in_progress", blocked)
		}
	})
}

func ids(issues []model.Issue) []model.ID {
	out := make([]model.ID, len(issues))
	for i, issue := range issues {
		out[i] = issue.ID
	}
	return out
}

// The clause: only a live BLOCKS edge out of a non-terminal Issue puts a key in
// the map. Each excluded branch gets its own would-be blocked Issue, so
// dropping any one of the three guards keys an Issue that is not blocked.
func TestOpenBlockersCountOnlyLiveBlocksEdgesFromLiveIssues(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "real", "wall", "hasrel", "hasdead", "gone")
	blockingGraph(t, rules,
		[]model.ID{"real", "wall", "hasrel", "hasdead", "gone"},
		[][2]model.ID{{"real", "wall"}, {"gone", "wall"}},
	)
	// hasrel points at wall, but a related edge is not a blocking one.
	if _, err := rules.SetDependency(t.Context(), "hasrel", "wall", model.DepRelated, true); err != nil {
		t.Fatalf("relate hasrel to wall: %v", err)
	}
	// hasdead's blocks edge was removed, so it no longer blocks.
	for _, present := range []bool{true, false} {
		if _, err := rules.SetDependency(t.Context(), "hasdead", "wall", model.DepBlocks, present); err != nil {
			t.Fatalf("set hasdead blocks edge to %v: %v", present, err)
		}
	}
	// gone's blocks edge is live and its blocker is open, but a closed Issue is
	// finished rather than waiting.
	if _, err := rules.SetIssueStatus(t.Context(), "gone", model.StatusClosed, "done"); err != nil {
		t.Fatalf("close gone: %v", err)
	}

	blockers, err := rules.OpenBlockers(t.Context())
	if err != nil {
		t.Fatalf("open blockers: %v", err)
	}
	if want := []model.ID{"wall"}; !slices.Equal(blockers["real"], want) {
		t.Errorf("blockers[real] = %v, want %v", blockers["real"], want)
	}
	for _, id := range []model.ID{"hasrel", "hasdead", "gone", "wall"} {
		if got := blockers[id]; len(got) != 0 {
			t.Errorf("blockers[%s] = %v, want none", id, got)
		}
	}
}

// The clause: a limit caps the ANSWER, not the candidate pool. The store
// applies Limit as SQL LIMIT before either verb has partitioned anything, so a
// pool capped at N returns fewer than N whenever the first N candidates are not
// all on the side being asked for. The graph interleaves ready and blocked in
// candidate order — priority, then created_at DESC — so both verbs see a
// disqualifying row inside the first two.
func TestReadyAndBlockedLimitTheAnswerNotTheCandidatePool(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "wall", "blockedb", "freeb", "blockeda", "freea")
	project := blockingGraph(t, rules,
		[]model.ID{"wall", "blockedb", "freeb", "blockeda", "freea"},
		[][2]model.ID{{"blockeda", "wall"}, {"blockedb", "wall"}},
	)
	filter := core.IssueFilter{Project: &project, Limit: 2}

	ready, err := rules.Ready(t.Context(), filter)
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if want := []model.ID{"freea", "freeb"}; !slices.Equal(ids(ready), want) {
		t.Errorf("ready = %v, want %v: two answers, not the ready half of the first two rows", ids(ready), want)
	}

	blocked, err := rules.Blocked(t.Context(), filter)
	if err != nil {
		t.Fatalf("blocked: %v", err)
	}
	got := make([]model.ID, len(blocked))
	for i, one := range blocked {
		got[i] = one.Issue.ID
	}
	if want := []model.ID{"blockeda", "blockedb"}; !slices.Equal(got, want) {
		t.Errorf("blocked = %v, want %v: two answers, not the blocked half of the first two rows", got, want)
	}
}
