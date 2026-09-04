package view_test

import (
	"reflect"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
	"github.com/tedkulp/drops/internal/view"
)

// issueView is the smallest view that renders: everything a Page reads is set
// explicitly, so a test below can add exactly the one relation it is about.
func issueView() core.IssueView {
	return core.IssueView{
		Issue: model.Issue{
			ID:       "k3f9x",
			Title:    "Build the render package",
			Type:     model.TypeTask,
			Status:   model.StatusOpen,
			Priority: 2,
		},
		Project:      model.Project{Slug: "drops"},
		Children:     []core.IssueRef{},
		Dependencies: []core.DependencyRef{},
		Dependents:   []core.DependencyRef{},
		Labels:       []model.Label{},
		Comments:     []model.Comment{},
	}
}

func dependency(id model.ID, title string, edge model.DependencyType) core.DependencyRef {
	return core.DependencyRef{
		IssueRef: core.IssueRef{ID: id, Title: title, Status: model.StatusOpen},
		Type:     edge,
	}
}

func refIDs(refs []render.Ref) []model.ID {
	ids := make([]model.ID, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	return ids
}

func assertIDs(t *testing.T, what string, got []render.Ref, want ...model.ID) {
	t.Helper()
	ids := refIDs(got)
	if len(ids) != len(want) {
		t.Fatalf("%s = %v, want %v", what, ids, want)
	}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("%s = %v, want %v", what, ids, want)
		}
	}
}

// TestPageCarriesIdentityBodyAndRelations pins what the one adapter produces:
// the issue's own fields, its nullable columns flattened, its labels by name,
// and each relation block in the order core handed it over.
func TestPageCarriesIdentityBodyAndRelations(t *testing.T) {
	assignee, reason := "agent", "done"
	closed, deferred := model.Timestamp("2026-09-02T08:00:00Z"), model.Timestamp("2026-09-09T08:00:00Z")

	source := issueView()
	source.Issue.Description = "the body"
	source.Issue.Status = model.StatusClosed
	source.Issue.Tombstone = model.Tombstoned
	source.Issue.Assignee = &assignee
	source.Issue.CloseReason = &reason
	source.Issue.ClosedAt = &closed
	source.Issue.DeferredUntil = &deferred
	source.Issue.CreatedAt = "2026-09-01T10:04:00Z"
	source.Issue.UpdatedAt = "2026-09-02T08:15:33Z"
	source.Labels = []model.Label{{Name: "wayfinder:task"}, {Name: "render"}}
	source.Parent = &core.IssueRef{ID: "k3f9x.0", Title: "the parent", Status: model.StatusOpen}
	source.Children = []core.IssueRef{{ID: "k3f9x.1", Title: "a child", Status: model.StatusOpen}}

	page := view.Page(source)

	want := render.Page{
		ID:            "k3f9x",
		Project:       "drops",
		Title:         "Build the render package",
		Status:        model.StatusClosed,
		Tombstoned:    true,
		Type:          model.TypeTask,
		Priority:      2,
		Assignee:      "agent",
		Labels:        []string{"wayfinder:task", "render"},
		CreatedAt:     "2026-09-01T10:04:00Z",
		UpdatedAt:     "2026-09-02T08:15:33Z",
		ClosedAt:      closed,
		DeferredUntil: deferred,
		CloseReason:   "done",
		Description:   "the body",
	}
	got := page
	got.Parent, got.Blockers, got.Blocking, got.Children, got.Comments = nil, nil, nil, nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("page identity and body\n got %+v\nwant %+v", got, want)
	}
	if page.Parent == nil || page.Parent.ID != "k3f9x.0" {
		t.Errorf("parent = %+v, want k3f9x.0", page.Parent)
	}
	assertIDs(t, "children", page.Children, "k3f9x.1")
}

// TestPageLeavesAbsentColumnsEmpty: every nullable column a page reads is a
// pointer, and an absent one renders as nothing rather than panicking.
func TestPageLeavesAbsentColumnsEmpty(t *testing.T) {
	page := view.Page(issueView())
	if page.Assignee != "" || page.CloseReason != "" || page.ClosedAt != "" || page.DeferredUntil != "" {
		t.Errorf("an absent column rendered something: %+v", page)
	}
	if page.Parent != nil {
		t.Errorf("parent = %+v, want nil", page.Parent)
	}
	// Never nil, so a caller emitting JSON derives [] from an empty block.
	if page.Blockers == nil || page.Blocking == nil || page.Children == nil {
		t.Errorf("an empty relation block is nil rather than empty: %+v", page)
	}
}

// TestPageNamesOnlyBlocksEdges: core hands over every edge type in one block,
// so `related` and `discovered-from` are dropped here. An edge rendered under
// "Blocked by" would report a link that does not block as one that does, and
// `ready` would disagree with the page.
func TestPageNamesOnlyBlocksEdges(t *testing.T) {
	source := issueView()
	source.Dependencies = []core.DependencyRef{
		dependency("k3f9x.1", "a real blocker", model.DepBlocks),
		dependency("k3f9x.2", "merely related", model.DepRelated),
		dependency("k3f9x.3", "where this came from", model.DepDiscoveredFrom),
	}
	source.Dependents = []core.DependencyRef{
		dependency("k3f9x.4", "waiting on this", model.DepBlocks),
		dependency("k3f9x.5", "merely related", model.DepRelated),
	}

	page := view.Page(source)
	assertIDs(t, "blockers", page.Blockers, "k3f9x.1")
	assertIDs(t, "blocking", page.Blocking, "k3f9x.4")
}

// TestPageNamesEachDirectionFromTheReadersEnd: the two headings are named from
// the reader's end, which is the opposite of the edge's own. An OUTGOING edge
// is one this issue depends on, so it BLOCKS this issue; an incoming edge is
// one that depends on this issue, so this issue blocks it. Swapping them
// reverses every dependency `show` prints while still printing two blocks.
func TestPageNamesEachDirectionFromTheReadersEnd(t *testing.T) {
	source := issueView()
	source.Dependencies = []core.DependencyRef{dependency("k3f9x.1", "what blocks me", model.DepBlocks)}
	source.Dependents = []core.DependencyRef{dependency("k3f9x.2", "what I block", model.DepBlocks)}

	page := view.Page(source)
	assertIDs(t, "blockers", page.Blockers, "k3f9x.1")
	assertIDs(t, "blocking", page.Blocking, "k3f9x.2")
}

// TestPageCarriesItsWholeThread: a page is identity, body, every relation AND
// the whole comment thread, oldest first, with each comment's id — the one
// argument `comment rm` takes.
func TestPageCarriesItsWholeThread(t *testing.T) {
	source := issueView()
	source.Comments = []model.Comment{
		{ID: "c1", Author: "agent", Body: "the first remark", CreatedAt: "2026-09-01T10:00:00Z"},
		{ID: "c2", Author: "agent", Body: "the second remark", CreatedAt: "2026-09-02T10:00:00Z"},
	}

	page := view.Page(source)
	want := []render.Comment{
		{ID: "c1", Author: "agent", Body: "the first remark", CreatedAt: "2026-09-01T10:00:00Z"},
		{ID: "c2", Author: "agent", Body: "the second remark", CreatedAt: "2026-09-02T10:00:00Z"},
	}
	if len(page.Comments) != len(want) {
		t.Fatalf("thread = %+v, want %+v", page.Comments, want)
	}
	for index := range want {
		if page.Comments[index] != want[index] {
			t.Fatalf("thread = %+v, want %+v", page.Comments, want)
		}
	}
}

// TestPageMarksATombstonedRelation: a tombstone is a state, not an absence.
// core keeps the relation and marks it, and the mark has to survive the
// adapter or a removed child renders as a live one.
func TestPageMarksATombstonedRelation(t *testing.T) {
	source := issueView()
	source.Children = []core.IssueRef{
		{ID: "k3f9x.1", Title: "a live child", Status: model.StatusOpen},
		{ID: "k3f9x.2", Title: "a removed child", Status: model.StatusOpen, Tombstoned: true},
	}

	page := view.Page(source)
	if len(page.Children) != 2 {
		t.Fatalf("children = %+v, want both", page.Children)
	}
	if page.Children[0].Tombstoned || !page.Children[1].Tombstoned {
		t.Errorf("the tombstone did not survive the adapter: %+v", page.Children)
	}
}
