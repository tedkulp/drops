package core_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// steppingClock reads a minute later every time it is asked, so every write a
// test makes lands on its own whole-minute timestamp. The order of closes is
// then the order the test wrote them in, and nothing rides on how RFC3339Nano
// text happens to sort within one second.
type steppingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *steppingClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(time.Minute)
	return clock.now
}

// closedCore is a core whose clock steps, over a fresh store, with its ids
// named.
func closedCore(t *testing.T, ids ...model.ID) *core.Core {
	t.Helper()
	_, opened := openCore(t)
	replica, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	return core.New(opened, replica, &steppingClock{now: fixedNow},
		&sequenceIDs{values: ids}, nil)
}

func createIn(t *testing.T, rules *core.Core, project model.ProjectKey, title string, priority int) model.Issue {
	t.Helper()
	created, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project, Title: title, Type: model.TypeTask, Priority: priority,
	})
	if err != nil {
		t.Fatalf("create %s: %v", title, err)
	}
	return created
}

func closeIssue(t *testing.T, rules *core.Core, id model.ID) {
	t.Helper()
	if _, err := rules.SetIssueStatus(t.Context(), id, model.StatusClosed, "done"); err != nil {
		t.Fatalf("close %s: %v", id, err)
	}
}

func closedIDs(t *testing.T, rules *core.Core, project *model.ProjectKey) []model.ID {
	t.Helper()
	closed, err := rules.ClosedIssues(t.Context(), project)
	if err != nil {
		t.Fatalf("closed issues: %v", err)
	}
	return ids(closed)
}

// The clause: Closed is most recently closed first. The close order is chosen
// to disagree with every other order the store knows — id, creation, and the
// queue's priority-then-newest — so a read that fell back on any of them
// returns a different list rather than the same one by luck.
func TestClosedIssuesAreMostRecentlyClosedFirst(t *testing.T) {
	rules := closedCore(t, "aaa", "bbb", "ccc")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	createIn(t, rules, project.Key, "aaa", 1)
	createIn(t, rules, project.Key, "bbb", 3)
	createIn(t, rules, project.Key, "ccc", 2)

	closeIssue(t, rules, "bbb")
	closeIssue(t, rules, "aaa")
	closeIssue(t, rules, "ccc")

	if got, want := closedIDs(t, rules, &project.Key), []model.ID{"ccc", "aaa", "bbb"}; !slices.Equal(got, want) {
		t.Fatalf("closed = %v, want %v: most recently closed first", got, want)
	}
}

// The clause: two issues closed at the same instant come back in id order.
// Two cores over one store, each with a clock stopped at the same moment,
// are how two closes share a closed_at exactly — which is what two replicas
// closing at once and a sync import produce.
func TestClosedIssuesBreakATieOnID(t *testing.T) {
	_, opened := openCore(t)
	replica, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	writer := core.New(opened, replica, &steppingClock{now: fixedNow},
		&sequenceIDs{values: []model.ID{"aaa", "bbb"}}, nil)
	project, err := writer.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	createIn(t, writer, project.Key, "aaa", 2)
	createIn(t, writer, project.Key, "bbb", 2)

	// Closed b-first, so the write order and the id order disagree.
	later := fixedClock{now: fixedNow.Add(24 * time.Hour)}
	for _, id := range []model.ID{"bbb", "aaa"} {
		closer := core.New(opened, replica, later, nil, nil)
		closeIssue(t, closer, id)
	}
	a, _ := writer.Issue(t.Context(), "aaa")
	b, _ := writer.Issue(t.Context(), "bbb")
	if a.ClosedAt == nil || b.ClosedAt == nil || *a.ClosedAt != *b.ClosedAt {
		t.Fatalf("closed_at = %v and %v; the fixture did not produce a tie", a.ClosedAt, b.ClosedAt)
	}

	if got, want := closedIDs(t, writer, &project.Key), []model.ID{"aaa", "bbb"}; !slices.Equal(got, want) {
		t.Fatalf("closed = %v, want %v: a tie on closed_at breaks on id", got, want)
	}
}

// The clause: the read is scoped to one project, or spans every project when
// no project is named — the board's `a`.
func TestClosedIssuesAreScopedToTheProjectOrSpanTheStore(t *testing.T) {
	rules := closedCore(t, "here", "there")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	other, err := rules.CreateProject(t.Context(), "beacon")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	createIn(t, rules, project.Key, "here", 2)
	createIn(t, rules, other.Key, "there", 2)
	closeIssue(t, rules, "here")
	closeIssue(t, rules, "there")

	if got, want := closedIDs(t, rules, &project.Key), []model.ID{"here"}; !slices.Equal(got, want) {
		t.Fatalf("scoped closed = %v, want %v", got, want)
	}
	if got, want := closedIDs(t, rules, nil), []model.ID{"there", "here"}; !slices.Equal(got, want) {
		t.Fatalf("store-wide closed = %v, want %v", got, want)
	}
}

// The clause: only closed live issues. An open and an in_progress issue stay
// out, and so does a closed issue that has been tombstoned.
func TestClosedIssuesLeaveOutLiveAndTombstonedIssues(t *testing.T) {
	rules := closedCore(t, "open", "doing", "done", "gone")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, title := range []string{"open", "doing", "done", "gone"} {
		createIn(t, rules, project.Key, title, 2)
	}
	if _, err := rules.SetIssueStatus(t.Context(), "doing", model.StatusInProgress, ""); err != nil {
		t.Fatalf("start doing: %v", err)
	}
	closeIssue(t, rules, "done")
	closeIssue(t, rules, "gone")
	if _, err := rules.SetIssueTombstone(t.Context(), "gone", model.Tombstoned); err != nil {
		t.Fatalf("tombstone gone: %v", err)
	}

	if got, want := closedIDs(t, rules, &project.Key), []model.ID{"done"}; !slices.Equal(got, want) {
		t.Fatalf("closed = %v, want %v: closed and live only", got, want)
	}
}
