package tui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// Every test in this package runs against real temp SQLite. AGENTS.md is
// explicit that the datastore is not a discovered fact and gets no interface;
// what IS a discovered fact here — terminal size — arrives as a message, and
// the frame tests pin it with tea.WithWindowSize or by setting it directly.

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

// namedIDs hands out the ids a fixture asks for, in order, so a test that
// cares what an id looks like can say so; everything after them is a short
// deterministic id, because a random one would make a frame unrepeatable.
type namedIDs struct {
	values []model.ID
	minted int
}

func (source *namedIDs) NewID(_ model.OwnerKind, _ *model.ID, _ []model.ID) (model.ID, error) {
	if len(source.values) > 0 {
		id := source.values[0]
		source.values = source.values[1:]
		return id, nil
	}
	source.minted++
	return model.ID(fmt.Sprintf("iss%02d", source.minted)), nil
}

// fixture is one temp store, its core, and the project the pane is scoped to.
type fixture struct {
	t       *testing.T
	core    *core.Core
	project model.Project
	other   model.Project
}

func newFixture(t *testing.T, ids ...model.ID) *fixture {
	t.Helper()
	opened, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "drops.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	replica, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	rules := core.New(opened, replica,
		fixedClock{now: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)},
		&namedIDs{values: ids}, nil)

	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	other, err := rules.CreateProject(t.Context(), "beacon")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &fixture{t: t, core: rules, project: project, other: other}
}

// issue writes one issue into the pane's own project.
func (f *fixture) issue(title string, priority int) model.Issue {
	f.t.Helper()
	return f.issueIn(f.project, title, priority)
}

// issueWith writes an issue carrying a body, which is how a test reaches the
// lines render.Console.Page deliberately does not reflow.
func (f *fixture) issueWith(title, description string, priority int) model.Issue {
	f.t.Helper()
	created, err := f.core.CreateIssue(f.t.Context(), core.CreateIssue{
		Project:     f.project.Key,
		Title:       title,
		Description: description,
		Type:        model.TypeTask,
		Priority:    priority,
	})
	if err != nil {
		f.t.Fatalf("create issue %q: %v", title, err)
	}
	return created
}

func (f *fixture) issueIn(project model.Project, title string, priority int) model.Issue {
	f.t.Helper()
	created, err := f.core.CreateIssue(f.t.Context(), core.CreateIssue{
		Project:  project.Key,
		Title:    title,
		Type:     model.TypeTask,
		Priority: priority,
	})
	if err != nil {
		f.t.Fatalf("create issue %q: %v", title, err)
	}
	return created
}

func (f *fixture) setStatus(id model.ID, status model.Status) {
	f.t.Helper()
	if _, err := f.core.SetIssueStatus(f.t.Context(), id, status, ""); err != nil {
		f.t.Fatalf("set status of %s: %v", id, err)
	}
}

// blocks records that blocked depends on blocker, which is what puts the
// ` [N]` marker on blocked's row.
func (f *fixture) blocks(blocked, blocker model.ID) {
	f.t.Helper()
	if _, err := f.core.SetDependency(f.t.Context(), blocked, blocker, model.DepBlocks, true); err != nil {
		f.t.Fatalf("dep %s -> %s: %v", blocked, blocker, err)
	}
}

// model builds the navigator over this fixture, sized and loaded, exactly as
// Run would have left it before the program started.
func (f *fixture) model(width, height int) *Model {
	f.t.Helper()
	pane := New(f.core, f.project, "tester")
	pane.ctx = f.t.Context()
	pane.width, pane.height = width, height
	pane.resize()
	if err := pane.reload(); err != nil {
		f.t.Fatalf("reload: %v", err)
	}
	return pane
}
