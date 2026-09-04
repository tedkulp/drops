package store_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// legacyIDs are the three permanent ID shapes the store must carry verbatim.
var legacyIDs = []model.ID{"k3f9x", "br-6vf", "beacon-ci-never-executed-4bhg", "dw32p.19"}

func TestIssueRoundTripsEveryFieldIncludingLegacyIDShapes(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")

	for _, id := range legacyIDs {
		want := newIssue(t, project, id, "carry every column")
		want.Description = "body\nwith\nlines"
		want.Type = model.TypeDecision
		want.Status = model.StatusClosed
		want.Priority = 0
		want.Assignee = text("ted")
		want.CloseReason = text("answered")
		want.DeferredUntil = stamp("2026-12-01T00:00:00Z")
		want.ClosedAt = stamp("2026-09-01T12:00:00Z")
		want.StartedAt = stamp("2026-08-30T08:00:00Z")
		want.Tombstone = model.Tombstoned

		write(t, opened, func(ctx context.Context, tx *store.Tx) error {
			if err := tx.PutIDOwner(ctx, model.IDOwner{
				ID: id, Kind: model.OwnerIssue, CreationReplica: project.CreationReplica,
			}); err != nil {
				return err
			}
			return tx.PutIssue(ctx, want)
		})

		got, err := opened.Issue(t.Context(), id)
		if err != nil {
			t.Fatalf("read Issue %s: %v", id, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Issue %s round trip:\n got %+v\nwant %+v", id, got, want)
		}
	}
}

// TestOptionalIssueColumnsStayNull separates "no assignee" from "the empty
// assignee". Storing an empty string for either would lose that difference on
// the mirror round trip.
func TestOptionalIssueColumnsStayNull(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")

	unset := seedIssue(t, opened, project, "unset", "nothing optional set")
	if got, err := opened.Issue(t.Context(), unset.ID); err != nil {
		t.Fatalf("read Issue: %v", err)
	} else if got.Assignee != nil || got.CloseReason != nil || got.ClosedAt != nil ||
		got.StartedAt != nil || got.DeferredUntil != nil {
		t.Errorf("unset optional columns read back non-nil: %+v", got)
	}

	empty := newIssue(t, project, "empty", "assigned to the empty string")
	empty.Assignee = text("")
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID: empty.ID, Kind: model.OwnerIssue, CreationReplica: project.CreationReplica,
		}); err != nil {
			return err
		}
		return tx.PutIssue(ctx, empty)
	})
	got, err := opened.Issue(t.Context(), empty.ID)
	if err != nil {
		t.Fatalf("read Issue: %v", err)
	}
	if got.Assignee == nil || *got.Assignee != "" {
		t.Errorf("empty assignee read back as %v, want a pointer to \"\"", got.Assignee)
	}
}

func TestPutIssueRequiresAReservedIDAndAKnownProject(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")

	unreserved := newIssue(t, project, "unreserved", "no owner row")
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutIssue(ctx, unreserved)
	}); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("PutIssue with an unreserved ID: err = %v, want ErrNotFound", err)
	}

	unknownProject := newIssue(t, project, "orphan", "no such Project")
	unknownProject.ProjectKey = newProjectKey(t)
	if err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID: "orphan", Kind: model.OwnerIssue, CreationReplica: project.CreationReplica,
		}); err != nil {
			return err
		}
		return tx.PutIssue(ctx, unknownProject)
	}); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("PutIssue into an absent Project: err = %v, want ErrNotFound", err)
	}
}

// TestIDOwnershipIsStructural pins the one invariant v7 moved out of doctor: an
// ID reserved to an Issue cannot be reused by a Memory, and the composite
// foreign key refuses it rather than a sweep reporting it afterwards.
func TestIDOwnershipIsStructural(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")
	issue := seedIssue(t, opened, project, "shared", "an Issue owns this ID")

	memory := newMemory(t, project, issue.ID, "collision", "body")
	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutMemory(ctx, memory)
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("Memory reusing an Issue's ID: err = %v, want ErrConflict", err)
	}

	reReserve := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutIDOwner(ctx, model.IDOwner{
			ID: issue.ID, Kind: model.OwnerMemory, CreationReplica: project.CreationReplica,
		})
	})
	if !errors.Is(reReserve, model.ErrConflict) {
		t.Errorf("reserving a taken ID for the other kind: err = %v, want ErrConflict", reReserve)
	}

	owner, err := opened.IDOwner(t.Context(), issue.ID)
	if err != nil {
		t.Fatalf("read ID owner: %v", err)
	}
	if owner.Kind != model.OwnerIssue {
		t.Errorf("owner kind = %s, want issue", owner.Kind)
	}
}

func TestPutIssueRefusesValuesOutsideTheVocabulary(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")

	cases := map[string]func(*model.Issue){
		"unknown type":    func(issue *model.Issue) { issue.Type = "spike" },
		"retired status":  func(issue *model.Issue) { issue.Status = "blocked" },
		"deleted status":  func(issue *model.Issue) { issue.Status = "deleted" },
		"priority above":  func(issue *model.Issue) { issue.Priority = 5 },
		"priority below":  func(issue *model.Issue) { issue.Priority = -1 },
		"empty title":     func(issue *model.Issue) { issue.Title = "" },
		"empty id":        func(issue *model.Issue) { issue.ID = "" },
		"bad project key": func(issue *model.Issue) { issue.ProjectKey = "drops" },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			invalid := newIssue(t, project, model.ID("candidate-"+name), "valid")
			breakIt(&invalid)
			err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
				return tx.PutIssue(ctx, invalid)
			})
			if !errors.Is(err, model.ErrInvalid) {
				t.Errorf("PutIssue with a %s: err = %v, want ErrInvalid", name, err)
			}
		})
	}
}

func TestUpdateIssueIsACompareAndSwap(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	issue := seedIssue(t, opened, project, "k3f9x", "original")

	winner := issue
	winner.Title = "the winner"
	winner.Revision, _ = issue.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateIssue(ctx, winner, issue.Revision)
	})

	loser := issue
	loser.Title = "the loser"
	loser.Revision, _ = issue.Revision.Next(replica)
	err := writeErr(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateIssue(ctx, loser, issue.Revision)
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("update from a stale revision: err = %v, want ErrConflict", err)
	}

	stored, err := opened.Issue(t.Context(), issue.ID)
	if err != nil {
		t.Fatalf("read Issue: %v", err)
	}
	if stored.Title != winner.Title {
		t.Errorf("stored title = %q, want %q", stored.Title, winner.Title)
	}
}

// TestTombstonedIssuesAreStillAddressable pins that a tombstone is a state, not
// an absence: show still answers, and only listings hide it.
func TestTombstonedIssuesAreStillAddressable(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	issue := seedIssue(t, opened, project, "gone", "removed")

	removed := issue
	removed.Tombstone = model.Tombstoned
	removed.Revision, _ = issue.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateIssue(ctx, removed, issue.Revision)
	})

	stored, err := opened.Issue(t.Context(), issue.ID)
	if err != nil {
		t.Fatalf("read tombstoned Issue: %v", err)
	}
	if stored.Tombstone != model.Tombstoned {
		t.Error("tombstone did not survive the round trip")
	}

	live, err := opened.Issues(t.Context(), store.IssueFilter{})
	if err != nil {
		t.Fatalf("list Issues: %v", err)
	}
	if slices.Contains(issueIDs(live), issue.ID) {
		t.Error("default listing includes a tombstoned Issue")
	}

	all, err := opened.Issues(t.Context(), store.IssueFilter{IncludeTombstoned: true})
	if err != nil {
		t.Fatalf("list Issues: %v", err)
	}
	if !slices.Contains(issueIDs(all), issue.ID) {
		t.Error("IncludeTombstoned listing omits a tombstoned Issue")
	}
}

func TestIssuesFilterAndOrder(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	drops := seedProject(t, opened, replica, "drops")
	beacon := seedProject(t, opened, replica, "beacon")

	critical := newIssue(t, drops, "critical", "critical work")
	critical.Priority = 0
	backlog := newIssue(t, drops, "backlog", "someday")
	backlog.Priority = 4
	backlog.Type = model.TypeResearch
	assigned := newIssue(t, drops, "assigned", "claimed")
	assigned.Priority = 2
	assigned.Assignee = text("ted")
	elsewhere := newIssue(t, beacon, "elsewhere", "another Project")

	for _, issue := range []model.Issue{critical, backlog, assigned, elsewhere} {
		write(t, opened, func(ctx context.Context, tx *store.Tx) error {
			if err := tx.PutIDOwner(ctx, model.IDOwner{
				ID: issue.ID, Kind: model.OwnerIssue, CreationReplica: replica,
			}); err != nil {
				return err
			}
			return tx.PutIssue(ctx, issue)
		})
	}
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		revision, _ := model.InitialRevision(replica)
		return tx.PutLabel(ctx, model.Label{IssueID: "backlog", Name: "deferred", Revision: revision})
	})

	cases := []struct {
		name   string
		filter store.IssueFilter
		want   []model.ID
	}{
		{"whole store in priority order", store.IssueFilter{},
			[]model.ID{"critical", "assigned", "elsewhere", "backlog"}},
		{"one Project", store.IssueFilter{Project: &drops.Key},
			[]model.ID{"critical", "assigned", "backlog"}},
		{"one type", store.IssueFilter{Types: []model.IssueType{model.TypeResearch}},
			[]model.ID{"backlog"}},
		{"two priorities", store.IssueFilter{Priorities: []int{0, 4}},
			[]model.ID{"critical", "backlog"}},
		{"one assignee", store.IssueFilter{Assignee: text("ted")},
			[]model.ID{"assigned"}},
		{"unassigned", store.IssueFilter{Assignee: text(""), Project: &drops.Key},
			[]model.ID{"critical", "backlog"}},
		{"by label", store.IssueFilter{AnyLabels: []string{"deferred"}},
			[]model.ID{"backlog"}},
		{"limited", store.IssueFilter{Limit: 2},
			[]model.ID{"critical", "assigned"}},
		{"no match", store.IssueFilter{Statuses: []model.Status{model.StatusClosed}},
			[]model.ID{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			listed, err := opened.Issues(t.Context(), testCase.filter)
			if err != nil {
				t.Fatalf("list Issues: %v", err)
			}
			if got := issueIDs(listed); !slices.Equal(got, testCase.want) {
				t.Errorf("Issues = %v, want %v", got, testCase.want)
			}
		})
	}
}

// TestIssuesByIDReadsEveryNamedIssueInOneQuery is the batch read a page's
// relations resolve through. It mirrors Issue rather than a listing: a
// tombstoned Issue is a state and still answers, and an id naming nothing is
// ErrNotFound, because a caller asks by ids it read off relation records whose
// foreign keys guarantee the row.
func TestIssuesByIDReadsEveryNamedIssueInOneQuery(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")

	live := seedIssue(t, opened, project, "k3f9x", "a live issue")
	removed := newIssue(t, project, "br-6vf", "a removed issue")
	removed.Tombstone = model.Tombstoned
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID: removed.ID, Kind: model.OwnerIssue, CreationReplica: project.CreationReplica,
		}); err != nil {
			return err
		}
		return tx.PutIssue(ctx, removed)
	})

	// A repeated id is accepted and answers once: a page's relations can name
	// one Issue from two sides.
	found, err := opened.IssuesByID(t.Context(), []model.ID{live.ID, removed.ID, live.ID})
	if err != nil {
		t.Fatalf("read Issues by id: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("IssuesByID returned %d Issues, want 2: %#v", len(found), found)
	}
	if found[live.ID].Title != "a live issue" {
		t.Errorf("live Issue = %#v", found[live.ID])
	}
	if got := found[removed.ID]; got.Tombstone != model.Tombstoned || got.Title != "a removed issue" {
		t.Errorf("a tombstoned Issue did not answer: %#v", got)
	}
}

// The clause: a batch read is not a listing, so an id naming no Issue is a
// fault the caller hears about rather than a key that is quietly absent.
func TestIssuesByIDRefusesAnIDNamingNoIssue(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")
	live := seedIssue(t, opened, project, "k3f9x", "a live issue")

	_, err := opened.IssuesByID(t.Context(), []model.ID{live.ID, "zzzzz"})
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("IssuesByID over an unknown id = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "zzzzz") {
		t.Errorf("the error does not name the missing id: %v", err)
	}
}

// An empty request is an empty answer and no query: a caller with no relations
// to resolve must not have to guard the call.
func TestIssuesByIDAnswersAnEmptyRequestWithAnEmptyMap(t *testing.T) {
	opened := newStore(t)

	found, err := opened.IssuesByID(t.Context(), nil)
	if err != nil {
		t.Fatalf("read no Issues: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("IssuesByID(nil) = %#v, want an empty map", found)
	}
}
