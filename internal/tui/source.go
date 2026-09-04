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

// issue reads one issue and every relation rendered with it, in core's own
// transaction, so a page and the picker over it cannot combine two revisions
// of the graph.
//
// The pane KEEPS this view rather than re-reading for `f`: the relation picker
// lists exactly what the page beside it was drawn from, and following a
// relation costs no extra read at all.
func (s *source) issue(ctx context.Context, id model.ID) (core.IssueView, error) {
	return s.core.ViewIssue(ctx, id)
}

// page renders one already-read issue's detail text at a pane width.
//
// render.Console into a buffer, unchanged: TTY false, no pager, the measured
// width. TTY gates Rows' truncation and nothing on a page; a nil Pager writes
// straight through. The page truncates nothing at all, which is why the
// viewport that clips it has to be scrollable — those bytes are off-screen,
// not gone.
func (s *source) page(issueView core.IssueView, width int) (string, error) {
	var buf bytes.Buffer
	console := render.Console{Out: &buf, Width: width, TTY: false}
	if err := console.Page(view.Page(issueView)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// seq is the store's write sequence: the change token the poll is built on.
//
// store_state.write_seq is bumped inside the same transaction as every
// replicated write — issues, comments, relations, labels, parents, projects,
// memories, locators — so a committed change can never be invisible to it, and
// a sync import bumps once per applied record while skipping no-op merges. It
// is one primary-key row: measured at 4.2µs against the 1025-issue corpus,
// against 5.24ms for OpenBlockers alone, so the poll costs 1/1300th of the
// refresh it decides not to make.
//
// It is deliberately COARSER than the pane: a memory write or a project rename
// moves it while nothing on screen changes. That direction is the safe one —
// it can raise a stale marker for nothing, never miss one.
func (s *source) seq(ctx context.Context) (int64, error) {
	state, err := s.core.State(ctx)
	if err != nil {
		return 0, err
	}
	return state.WriteSeq, nil
}
