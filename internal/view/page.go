// Package view turns one core.IssueView into what a reading surface shows.
//
// There is exactly one adapter from a core.IssueView to a render.Page, and it
// lives here because two callers need it: `show` in internal/cli, and the TUI's
// detail pane in internal/tui. cli imports tui and never the reverse
// (qy3de.3), so tui cannot reach back into cli for it, and a second spelling of
// this adapter is this repository's dominant defect class — two renderings of
// one issue that agree only until someone edits one of them.
//
// It is NOT folded into render, and that was the open question. render imports
// internal/model and nothing else, deliberately: that is what makes every byte
// it emits reproducible from a struct literal, with no store and no
// environment behind it. qy3de.9 left IssueView carrying resolved core.IssueRef
// and core.DependencyRef relations, so this adapter's input is a core type;
// folding it into render would make render import core, and core imports store.
package view

import (
	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// Page turns a core.IssueView into the render.Page a reading surface renders:
// identity, body, every relation, and the whole comment thread.
//
// Nothing here reads the store. The relations arrive from core already named
// and already ordered, read in the same transaction as the issue itself, so a
// page cannot combine two revisions of the graph — and a caller cannot
// reintroduce the N+1 by assembling one differently.
func Page(view core.IssueView) render.Page {
	labels := make([]string, 0, len(view.Labels))
	for _, label := range view.Labels {
		labels = append(labels, label.Name)
	}
	page := render.Page{
		ID:          view.Issue.ID,
		Project:     view.Project.Slug,
		Title:       view.Issue.Title,
		Status:      view.Issue.Status,
		Tombstoned:  view.Issue.Tombstone == model.Tombstoned,
		Type:        view.Issue.Type,
		Priority:    view.Issue.Priority,
		Labels:      labels,
		CreatedAt:   view.Issue.CreatedAt,
		UpdatedAt:   view.Issue.UpdatedAt,
		CloseReason: stringValue(view.Issue.CloseReason),
		Description: view.Issue.Description,
	}
	if view.Issue.Assignee != nil {
		page.Assignee = *view.Issue.Assignee
	}
	if view.Issue.ClosedAt != nil {
		page.ClosedAt = *view.Issue.ClosedAt
	}
	if view.Issue.DeferredUntil != nil {
		page.DeferredUntil = *view.Issue.DeferredUntil
	}
	for _, comment := range view.Comments {
		page.Comments = append(page.Comments, render.Comment{
			ID:        comment.ID,
			Author:    comment.Author,
			Body:      comment.Body,
			CreatedAt: comment.CreatedAt,
		})
	}

	if view.Parent != nil {
		parent := toRef(*view.Parent)
		page.Parent = &parent
	}
	// An outgoing edge is one this issue depends on, so it is what BLOCKS this
	// issue; an incoming edge is one that depends on this issue, so it is what
	// this issue blocks. The two directions are named from the reader's end,
	// which is the opposite of the edge's own.
	page.Blockers = toRefs(blocksRefs(view.Dependencies))
	page.Blocking = toRefs(blocksRefs(view.Dependents))
	page.Children = toRefs(view.Children)
	return page
}

// blocksRefs keeps the `blocks` edges of one dependency block. A page names
// blockers and nothing else, so a `related` or `discovered-from` edge is
// dropped here rather than rendered under a heading that would misreport it.
//
// The relations arrive live: store filters `tombstoned = 0` in SQL for labels,
// dependencies and children alike, and core drops a tombstoned parent record.
// Re-filtering that here would be a rule in the presentation layer that no
// input can ever reach, so it is not written.
func blocksRefs(dependencies []core.DependencyRef) []core.IssueRef {
	refs := make([]core.IssueRef, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.Type == model.DepBlocks {
			refs = append(refs, dependency.IssueRef)
		}
	}
	return refs
}

func toRef(ref core.IssueRef) render.Ref {
	return render.Ref{ID: ref.ID, Title: ref.Title, Status: ref.Status, Tombstoned: ref.Tombstoned}
}

// toRefs is never nil, so a relation block that is empty is empty rather than
// absent — which is what lets a caller emitting JSON derive [] from it.
func toRefs(refs []core.IssueRef) []render.Ref {
	out := make([]render.Ref, 0, len(refs))
	for _, ref := range refs {
		out = append(out, toRef(ref))
	}
	return out
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
