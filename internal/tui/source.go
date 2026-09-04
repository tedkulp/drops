package tui

import (
	"bytes"
	"context"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
	"github.com/tedkulp/drops/internal/view"
)

// source is every read the pane makes, and the only place it touches core.
type source struct {
	core    *core.Core
	project model.Project

	// slugs is the project-key to slug map the project column reads. It is
	// loaded once: projects are not created while a pane is open, and a
	// per-refresh reload would be a second store round trip for a table of
	// fourteen rows.
	slugs map[model.ProjectKey]string
}

// rows reads one scope's row set, in the CLI's order — the store's, never
// re-sorted — annotated with each issue's open-blocker count.
//
// Two reads' worth of work, whatever the scope: the issue listing, and one
// core.OpenBlockers for the whole store. The blocker map is deliberately
// unscoped: a blocker outside the pane's scope still blocks, so a scoped map
// would under-report.
func (s *source) rows(ctx context.Context, sc scope) ([]row, error) {
	if s.slugs == nil {
		projects, err := s.core.Projects(ctx)
		if err != nil {
			return nil, err
		}
		s.slugs = make(map[model.ProjectKey]string, len(projects))
		for _, project := range projects {
			s.slugs[project.Key] = project.Slug
		}
	}

	filter := core.IssueFilter{Statuses: sc.statuses()}
	if !sc.allProjects {
		key := s.project.Key
		filter.Project = &key
	}
	issues, err := s.core.Issues(ctx, filter)
	if err != nil {
		return nil, err
	}
	blockers, err := s.core.OpenBlockers(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]row, 0, len(issues))
	for _, issue := range issues {
		rows = append(rows, row{
			id:         issue.ID,
			project:    s.slugs[issue.ProjectKey],
			status:     issue.Status,
			priority:   issue.Priority,
			title:      issue.Title,
			tombstoned: issue.Tombstone == model.Tombstoned,
			blockedBy:  len(blockers[issue.ID]),
		})
	}
	return rows, nil
}

// page renders one issue's detail text at a pane width.
//
// render.Console into a buffer, unchanged: TTY false, no pager, the measured
// width. TTY gates Rows' truncation and nothing on a page; a nil Pager writes
// straight through. The page truncates nothing at all, which is why the
// viewport that clips it has to be scrollable — those bytes are off-screen,
// not gone.
func (s *source) page(ctx context.Context, id model.ID, width int) (string, error) {
	issueView, err := s.core.ViewIssue(ctx, id)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	console := render.Console{Out: &buf, Width: width, TTY: false}
	if err := console.Page(view.Page(issueView)); err != nil {
		return "", err
	}
	return buf.String(), nil
}
