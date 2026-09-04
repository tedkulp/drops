package core_test

import (
	"strconv"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// The clause: an Issue page is read in ONE transaction, so it cannot show an
// Issue at one revision beside a comment thread at another. Every collection is
// non-nil so the JSON shape is stable whether or not the Issue has any.
func TestViewIssueReadsTheWholePageAtOneRevision(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "parent", "child")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	parent, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "parent", Type: model.TypeEpic, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "child", Type: model.TypeTask, Priority: 2,
		Labels: []string{"core"},
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if _, err := rules.SetIssueParent(t.Context(), child.ID, &parent.ID); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	if _, err := rules.SetDependency(t.Context(), child.ID, parent.ID, model.DepBlocks, true); err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	if _, err := rules.AddComment(t.Context(), child.ID, "ted", "a remark on the page"); err != nil {
		t.Fatalf("add Comment: %v", err)
	}

	view, err := rules.ViewIssue(t.Context(), child.ID)
	if err != nil {
		t.Fatalf("view Issue: %v", err)
	}
	if view.Issue.ID != child.ID || view.Project.Key != project.Key {
		t.Fatalf("view identity = %s in %s", view.Issue.ID, view.Project.Key)
	}
	if view.Parent == nil || view.Parent.ID != parent.ID {
		t.Fatalf("view parent = %#v, want %s", view.Parent, parent.ID)
	}
	if len(view.Dependencies) != 1 || view.Dependencies[0].ID != parent.ID {
		t.Fatalf("dependencies = %#v", view.Dependencies)
	}
	if len(view.Labels) != 1 || view.Labels[0].Name != "core" {
		t.Fatalf("labels = %#v", view.Labels)
	}
	if len(view.Comments) != 1 {
		t.Fatalf("comments = %#v", view.Comments)
	}

	// The parent's own page sees the relation from the other side, and every
	// empty collection is an empty slice rather than nil.
	up, err := rules.ViewIssue(t.Context(), parent.ID)
	if err != nil {
		t.Fatalf("view parent: %v", err)
	}
	if up.Parent != nil {
		t.Errorf("parent view has a parent: %#v", up.Parent)
	}
	if len(up.Children) != 1 || up.Children[0].ID != child.ID {
		t.Fatalf("children = %#v", up.Children)
	}
	if len(up.Dependents) != 1 || up.Dependents[0].ID != child.ID {
		t.Fatalf("dependents = %#v", up.Dependents)
	}
	if up.Labels == nil || up.Comments == nil || up.Dependencies == nil {
		t.Fatalf("nil collection on a page with none: %#v", up)
	}
}

// The clause: a tombstoned parent relation is not a parent. The row survives
// for the mirror to merge, but the page must not show a link that was cut.
func TestViewIssueOmitsATombstonedParent(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "parent", "child")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	parent, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "parent", Type: model.TypeEpic, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "child", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if _, err := rules.SetIssueParent(t.Context(), child.ID, &parent.ID); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	if _, err := rules.SetIssueParent(t.Context(), child.ID, nil); err != nil {
		t.Fatalf("clear parent: %v", err)
	}

	view, err := rules.ViewIssue(t.Context(), child.ID)
	if err != nil {
		t.Fatalf("view Issue: %v", err)
	}
	if view.Parent != nil {
		t.Fatalf("view parent = %#v, want none after the relation was cut", view.Parent)
	}
}

// The clause (qy3de.9): every relation arrives NAMED — title, status and
// tombstone state — and named for the Issue at the FAR end of its edge. A
// dependency read off the near end would name the Issue you are already
// looking at, which is a wrong page that still renders.
func TestViewIssueNamesTheFarEndOfEveryRelation(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "parent", "subject", "blocker", "dependent")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	titles := map[model.ID]string{
		"parent":    "the parent",
		"subject":   "the subject",
		"blocker":   "the blocker",
		"dependent": "the dependent",
	}
	for _, id := range []model.ID{"parent", "subject", "blocker", "dependent"} {
		if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
			Project: project.Key, Title: titles[id], Type: model.TypeTask, Priority: 2,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	if _, err := rules.SetIssueParent(t.Context(), "subject", ptr(model.ID("parent"))); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	if _, err := rules.SetDependency(t.Context(), "subject", "blocker", model.DepBlocks, true); err != nil {
		t.Fatalf("block subject on blocker: %v", err)
	}
	if _, err := rules.SetDependency(t.Context(), "dependent", "subject", model.DepBlocks, true); err != nil {
		t.Fatalf("block dependent on subject: %v", err)
	}
	if _, err := rules.SetIssueStatus(t.Context(), "blocker", model.StatusClosed, "done"); err != nil {
		t.Fatalf("close the blocker: %v", err)
	}

	view, err := rules.ViewIssue(t.Context(), "subject")
	if err != nil {
		t.Fatalf("view Issue: %v", err)
	}
	if view.Parent == nil || view.Parent.ID != "parent" || view.Parent.Title != "the parent" {
		t.Fatalf("parent ref = %#v, want the parent named", view.Parent)
	}
	if len(view.Dependencies) != 1 {
		t.Fatalf("dependencies = %#v, want one", view.Dependencies)
	}
	outgoing := view.Dependencies[0]
	if outgoing.ID != "blocker" || outgoing.Title != "the blocker" {
		t.Errorf("an outgoing edge names %s %q, want the blocker at its far end", outgoing.ID, outgoing.Title)
	}
	if outgoing.Status != model.StatusClosed {
		t.Errorf("the blocker's status = %q, want the status it was read at", outgoing.Status)
	}
	if outgoing.Type != model.DepBlocks {
		t.Errorf("the edge type = %q, want it to travel with the far end", outgoing.Type)
	}
	if len(view.Dependents) != 1 {
		t.Fatalf("dependents = %#v, want one", view.Dependents)
	}
	incoming := view.Dependents[0]
	if incoming.ID != "dependent" || incoming.Title != "the dependent" {
		t.Errorf("an incoming edge names %s %q, want the dependent at its far end", incoming.ID, incoming.Title)
	}
	if incoming.Type != model.DepBlocks {
		t.Errorf("the incoming edge type = %q", incoming.Type)
	}

	// And the same relation from the parent's side.
	up, err := rules.ViewIssue(t.Context(), "parent")
	if err != nil {
		t.Fatalf("view parent: %v", err)
	}
	if len(up.Children) != 1 || up.Children[0].Title != "the subject" {
		t.Fatalf("children = %#v, want the subject named", up.Children)
	}
}

// The clause: a removed related Issue is still listed and is MARKED. `show`
// draws ⊘ from this flag and does not count such a child open, so a ref that
// forgot the tombstone would report a deleted child as live.
func TestViewIssueMarksATombstonedRelation(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "parent", "live", "dead")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	for _, id := range []model.ID{"parent", "live", "dead"} {
		if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
			Project: project.Key, Title: string(id), Type: model.TypeTask, Priority: 2,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	for _, child := range []model.ID{"live", "dead"} {
		if _, err := rules.SetIssueParent(t.Context(), child, ptr(model.ID("parent"))); err != nil {
			t.Fatalf("parent %s: %v", child, err)
		}
	}
	if _, err := rules.SetIssueTombstone(t.Context(), "dead", model.Tombstoned); err != nil {
		t.Fatalf("tombstone the child: %v", err)
	}

	view, err := rules.ViewIssue(t.Context(), "parent")
	if err != nil {
		t.Fatalf("view Issue: %v", err)
	}
	marks := map[model.ID]bool{}
	for _, child := range view.Children {
		marks[child.ID] = child.Tombstoned
	}
	if len(marks) != 2 {
		t.Fatalf("children = %#v, want both listed: a removed relation is marked, not withheld", view.Children)
	}
	if marks["live"] {
		t.Errorf("the live child is marked tombstoned")
	}
	if !marks["dead"] {
		t.Errorf("the removed child is not marked tombstoned")
	}
}

// The clause: a relation block is in NATURAL id order, so `.2` precedes `.10`.
// The store answers lexicographically, which puts a map's eleventh ticket
// second and is exactly when a reader stops trusting the listing.
func TestViewIssueOrdersChildrenNaturally(t *testing.T) {
	ids := []model.ID{"map"}
	for i := 1; i <= 11; i++ {
		ids = append(ids, model.ID("map."+strconv.Itoa(i)))
	}
	rules, _ := openCoreWithIDs(t, ids...)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	for _, id := range ids {
		if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
			Project: project.Key, Title: string(id), Type: model.TypeTask, Priority: 2,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	for _, id := range ids[1:] {
		if _, err := rules.SetIssueParent(t.Context(), id, ptr(model.ID("map"))); err != nil {
			t.Fatalf("parent %s: %v", id, err)
		}
	}

	view, err := rules.ViewIssue(t.Context(), "map")
	if err != nil {
		t.Fatalf("view Issue: %v", err)
	}
	if len(view.Children) != 11 {
		t.Fatalf("children = %d, want 11", len(view.Children))
	}
	for i, child := range view.Children {
		if want := model.ID("map." + strconv.Itoa(i+1)); child.ID != want {
			t.Fatalf("children in %v, want natural order", refIDs(view.Children))
		}
	}
}

// The clause: a dependency block is grouped by EDGE TYPE before its far ends
// are ordered, so a page's blockers stay together whatever their ids are and a
// `related` edge can never be read as one of them.
func TestViewIssueGroupsDependenciesByTypeThenNaturalID(t *testing.T) {
	ids := []model.ID{"subject", "zz.2", "zz.10", "aa.1"}
	rules, _ := openCoreWithIDs(t, ids...)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	for _, id := range ids {
		if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
			Project: project.Key, Title: string(id), Type: model.TypeTask, Priority: 2,
		}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	// "related" sorts after "blocks" by name, and the blocks edges are wired
	// in the wrong order on purpose so only the comparator can fix them.
	for _, edge := range []struct {
		to   model.ID
		kind model.DependencyType
	}{
		{"zz.10", model.DepBlocks},
		{"zz.2", model.DepBlocks},
		{"aa.1", model.DepRelated},
	} {
		if _, err := rules.SetDependency(t.Context(), "subject", edge.to, edge.kind, true); err != nil {
			t.Fatalf("add %s edge to %s: %v", edge.kind, edge.to, err)
		}
	}

	view, err := rules.ViewIssue(t.Context(), "subject")
	if err != nil {
		t.Fatalf("view Issue: %v", err)
	}
	want := []model.ID{"zz.2", "zz.10", "aa.1"}
	got := make([]model.ID, 0, len(view.Dependencies))
	for _, dependency := range view.Dependencies {
		got = append(got, dependency.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("dependencies = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dependencies = %v, want %v: blocks before related, naturally ordered inside each", got, want)
		}
	}
	if view.Dependencies[2].Type != model.DepRelated {
		t.Errorf("the related edge lost its type: %#v", view.Dependencies[2])
	}
}

func ptr[T any](value T) *T { return &value }

func refIDs(refs []core.IssueRef) []model.ID {
	out := make([]model.ID, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.ID)
	}
	return out
}
