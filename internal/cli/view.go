package cli

import (
	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
	"github.com/tedkulp/drops/internal/view"
)

// showRefJSON is one related issue in show --json: the same three fields the
// old build emitted, so a consumer's `.children[].title` keeps resolving.
type showRefJSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// showIssueJSON is the flat --json shape of show: the issue's own fields, the
// live label names, and the relation keys that never go absent.
type showIssueJSON struct {
	model.Issue
	Labels   []string        `json:"labels,omitempty"`
	Parent   *showRefJSON    `json:"parent"`
	Blockers []showRefJSON   `json:"blockers"`
	Blocking []showRefJSON   `json:"blocking"`
	Children []showRefJSON   `json:"children"`
	Comments []model.Comment `json:"comments"`
}

// assembleView turns one view into the page and the JSON object show renders.
// The relations arrive from core already named and already ordered, read in the
// same transaction as the issue itself, so nothing here reads the store.
//
// The JSON is derived from the PAGE rather than from the view a second time.
// There is one adapter (internal/view) and the machine surface reads its
// output, so the two renderings cannot disagree about which edges block, which
// relations are live, or what order they come in — they are the same slices.
func assembleView(issueView core.IssueView) (render.Page, showIssueJSON) {
	page := view.Page(issueView)
	out := showIssueJSON{
		Issue:    issueView.Issue,
		Labels:   page.Labels,
		Blockers: toJSONRefs(page.Blockers),
		Blocking: toJSONRefs(page.Blocking),
		Children: toJSONRefs(page.Children),
		Comments: issueView.Comments,
	}
	if page.Parent != nil {
		parent := toJSONRef(*page.Parent)
		out.Parent = &parent
	}
	return page, out
}

func toJSONRef(ref render.Ref) showRefJSON {
	return showRefJSON{ID: string(ref.ID), Title: ref.Title, Status: string(ref.Status)}
}

func toJSONRefs(refs []render.Ref) []showRefJSON {
	out := make([]showRefJSON, 0, len(refs))
	for _, ref := range refs {
		out = append(out, toJSONRef(ref))
	}
	return out
}
