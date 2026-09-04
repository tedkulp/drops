package cli

import (
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
