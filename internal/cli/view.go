package cli

import (
	"context"
	"strconv"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
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

// refData resolves one related issue to the fields a page or a JSON object
// names it by.
type refData struct {
	id         model.ID
	title      string
	status     model.Status
	tombstoned bool
}

// resolveRefs resolves ids to refData in natural id order, dropping duplicates.
// An unresolvable id is an error: foreign keys make a dangling edge impossible,
// so a failure here is a real fault, not a skipped row.
func resolveRefs(ctx context.Context, rules *core.Core, ids []model.ID) ([]refData, error) {
	seen := make(map[model.ID]bool, len(ids))
	out := make([]refData, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		issue, err := rules.Issue(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, refData{
			id:         issue.ID,
			title:      issue.Title,
			status:     issue.Status,
			tombstoned: issue.Tombstone == model.Tombstoned,
		})
	}
	naturalSortRefs(out)
	return out, nil
}

func naturalSortRefs(refs []refData) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && naturalLess(string(refs[j].id), string(refs[j-1].id)); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// naturalLess compares two ids as alternating runs of digits and non-digits,
// comparing digit runs numerically, so `<p>.2` precedes `<p>.10`.
func naturalLess(a, b string) bool {
	as, bs := idRuns(a), idRuns(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, xErr := strconv.Atoi(as[i])
		y, yErr := strconv.Atoi(bs[i])
		if xErr == nil && yErr == nil {
			if x != y {
				return x < y
			}
			continue
		}
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

func idRuns(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		j, digit := i, isDigit(s[i])
		for j < len(s) && isDigit(s[j]) == digit {
			j++
		}
		out = append(out, s[i:j])
		i = j
	}
	return out
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// pageForView turns a core.IssueView into the render.Page show renders.
func pageForView(view core.IssueView) render.Page {
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
	return page
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
