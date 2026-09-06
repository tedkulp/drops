package tui

import (
	"fmt"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// followChoice is one relation the picker can follow: the id it retargets the
// detail pane to, and the group it was listed under. It is built in the SAME
// pass as the picker's rows, so choices[picker.selected()] is aligned by
// construction rather than by a lookup that could drift (qy3de.6 §6).
type followChoice struct {
	id    model.ID
	group string
}

// relationGroup is one heading and the far ends listed under it.
type relationGroup struct {
	label string
	refs  []core.IssueRef
}

// relationGroups is the whole relation vocabulary the picker lists, in the
// order render's page prints it and under the page's own words, so the picker
// and the page cannot disagree about what a relation is called:
//
//	Parent · Blocked by · Blocks · Children · Discovered from · Discovered · Related
//
// Like the page beside it, the picker covers the full core graph. It reads the
// IssueView directly because the rendered Console buffer is intentionally
// opaque, so this separate mapping keeps its own controls.
//
// **Every direction is named from the READER's end, which is the opposite of
// the edge's own** — the inversion view.Page documents for `blocks`, extended
// to the other two types. An OUT edge (Dependencies) is one this issue depends
// on, so a `blocks` out-edge is what BLOCKS this issue and a `discovered-from`
// out-edge is where this issue was DISCOVERED FROM. An IN edge (Dependents) is
// one that depends on this issue, so a `blocks` in-edge is what this issue
// BLOCKS and a `discovered-from` in-edge is what was DISCOVERED from it. A
// swapped pair survives a round trip and reads plausibly, which is why each
// direction is a catalogued control rather than a comment.
//
// `related` is undirected, so it is ONE group merging both directions rather
// than a pair — and the merge itself is core's, not a second spelling of it
// here, which is precisely this repository's dominant defect class. core
// orders each half and names each far end once, so a reciprocal pair is one
// row and one choice (y7f6z). The direction fixture uses both stored halves
// because the corpus's single live `related` edge cannot prove the merge.
func relationGroups(issue core.IssueView) []relationGroup {
	parent := []core.IssueRef{}
	if issue.Parent != nil {
		parent = append(parent, *issue.Parent)
	}
	return []relationGroup{
		{"Parent", parent},
		{"Blocked by", refsOfType(issue.Dependencies, model.DepBlocks)},
		{"Blocks", refsOfType(issue.Dependents, model.DepBlocks)},
		{"Children", issue.Children},
		{"Discovered from", refsOfType(issue.Dependencies, model.DepDiscoveredFrom)},
		{"Discovered", refsOfType(issue.Dependents, model.DepDiscoveredFrom)},
		{"Related", core.RelatedRefs(issue)},
	}
}

// refsOfType keeps one edge type's far ends, in the order core sorted them:
// sortDependencyRefs groups by type before ordering ids, so filtering by type
// leaves that order intact.
func refsOfType(dependencies []core.DependencyRef, want model.DependencyType) []core.IssueRef {
	refs := make([]core.IssueRef, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.Type == want {
			refs = append(refs, dependency.IssueRef)
		}
	}
	return refs
}

// followRows builds the picker's rows and the choices behind them in one pass.
func followRows(issue core.IssueView) ([]pickerRow, []followChoice) {
	groups := relationGroups(issue)
	pad := idPad(groups)

	rows, choices := []pickerRow{}, []followChoice{}
	for _, group := range groups {
		for _, ref := range group.refs {
			rows = append(rows, pickerRow{Group: group.label, Text: followText(ref, pad)})
			choices = append(choices, followChoice{id: ref.ID, group: group.label})
		}
	}
	return rows, choices
}

// idPad sizes the id column to the widest id IN THIS PICKER, uncapped.
//
// This overturns the obvious inheritance of the left pane's twelve-column cap,
// and the corpus is emphatic about why (qy3de.6 §4). That cap is safe on a
// heterogeneous list; a picker's rows are by construction siblings, and dotted
// child ids share a long prefix — `gitops-vn4` has 16 live children of which
// SEVEN collapse to the same `gitops-vn4.…` at twelve columns, in a picker
// whose whole job is choosing among them. qy3de.4's never-truncate-an-
// identifier exception held there because `show` was one keypress away; it
// fails here because the picker IS the identification step. The cap buys
// nothing either: auto-sized, with no project or priority column, 73 of the
// corpus's 77 non-empty pickers still leave at least 22 columns of title.
func idPad(groups []relationGroup) int {
	pad := 0
	for _, group := range groups {
		for _, ref := range group.refs {
			pad = max(pad, render.Cols(string(ref.ID)))
		}
	}
	return pad
}

// followText composes one row, untruncated: the picker's view cuts it to the
// pane. Glyph, id, title — the kind is the group header, which costs one row
// and zero columns where a `blocked-by ` prefix would cost eleven off a budget
// that starts at fourteen.
//
// The glyphs are render's own, so a picker, a listing and a page can never
// disagree about what a state looks like. They earn their column here: 52% of
// relation targets in the corpus are closed, and every `discovered-from`
// target is, because provenance points into history.
//
// The row is cut as a WHOLE by the picker, which means a pane too narrow for
// the id column would cut the id as well as the title. Measured unreachable
// rather than assumed to be: across the corpus's 77 non-empty pickers the
// narrowest title budget at a 38-column pane is fourteen columns, on a picker
// holding one row.
func followText(ref core.IssueRef, pad int) string {
	lead := fmt.Sprintf("%s %-*s", render.StatusMark(ref.Status, ref.Tombstoned), pad, ref.ID)
	if ref.Title == "" {
		return lead
	}
	return lead + " " + ref.Title
}
