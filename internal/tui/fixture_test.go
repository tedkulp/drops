package tui

import (
	"fmt"
	"path/filepath"
	"strings"
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

// fixture is one temp store, its core, the project the pane is scoped to, and
// the two seams the write set spends: the channel core's warning sink is
// pointed at, and the argv that stands in for $EDITOR.
type fixture struct {
	t        *testing.T
	core     *core.Core
	project  model.Project
	opened   *store.Store
	other    model.Project
	warnings chan core.Warning
	editor   []string
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
	// The warning sink is wired the way cli wires it for `tui`: onto a
	// buffered channel the pane drains, because under an alt screen the
	// default stderr sink is invisible (qy3de.7 §5).
	warnings := make(chan core.Warning, 32)
	rules := core.New(opened, replica,
		fixedClock{now: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)},
		&namedIDs{values: ids},
		func(w core.Warning) { warnings <- w })

	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	other, err := rules.CreateProject(t.Context(), "beacon")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &fixture{
		t: t, core: rules, opened: opened, project: project, other: other, warnings: warnings,
		// The default editor writes nothing and exits 0, which is the
		// silent-abort case. A test that wants text says so with editorWrites.
		editor: shEditor(":"),
	}
}

// shEditor is an $EDITOR made of a shell script. The path drops arrives at is
// "$1", because exec.Command's argument after -c becomes the shell's $0.
//
// A real process rather than a fake: AGENTS.md keeps real subsystems real, and
// the whole question these tests answer is what an editor's file and exit code
// do to the store.
func shEditor(script string) []string { return []string{"sh", "-c", script, "sh"} }

// editorWrites is an $EDITOR that puts body in the file and exits 0.
func editorWrites(body string) []string {
	return shEditor("printf %s " + shellQuote(body) + ` > "$1"`)
}

// shellQuote wraps a string in single quotes for the script above.
func shellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
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

// child writes an issue under a parent, which is how a fixture reaches the
// picker's Children group and its auto-sized id column.
func (f *fixture) child(parent model.ID, title string, priority int) model.Issue {
	f.t.Helper()
	created, err := f.core.CreateIssue(f.t.Context(), core.CreateIssue{
		Project:  f.project.Key,
		Parent:   &parent,
		Title:    title,
		Type:     model.TypeTask,
		Priority: priority,
	})
	if err != nil {
		f.t.Fatalf("create child of %s: %v", parent, err)
	}
	return created
}

// dep records one typed edge, from depending on to. It reaches the two types
// `show` cannot read (drops://hxedy), which the picker deliberately can.
func (f *fixture) dep(from, to model.ID, kind model.DependencyType) {
	f.t.Helper()
	if _, err := f.core.SetDependency(f.t.Context(), from, to, kind, true); err != nil {
		f.t.Fatalf("dep %s -%s-> %s: %v", from, kind, to, err)
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

// tombstone deletes an issue, which is how a fixture reaches the one state
// the modal offers no rows for. It stays reachable by following.
func (f *fixture) tombstone(id model.ID) {
	f.t.Helper()
	if _, err := f.core.SetIssueTombstone(f.t.Context(), id, model.Tombstoned); err != nil {
		f.t.Fatalf("tombstone %s: %v", id, err)
	}
}

// claim assigns an issue from outside the pane, which is both a fixture state
// and the race qy3de.7 §10 names: a claim landing between the render and the
// `enter`.
func (f *fixture) claim(id model.ID, who string) {
	f.t.Helper()
	if _, err := f.core.ClaimIssue(f.t.Context(), id, who); err != nil {
		f.t.Fatalf("claim %s: %v", id, err)
	}
}

// close closes an issue with a reason from outside the pane, which is how a
// fixture reaches the state `reopen` is offered from.
func (f *fixture) close(id model.ID, reason string) {
	f.t.Helper()
	if _, err := f.core.SetIssueStatus(f.t.Context(), id, model.StatusClosed, reason); err != nil {
		f.t.Fatalf("close %s: %v", id, err)
	}
}

// reread reads one issue back out of the store, which is how a write is
// asserted: through core, never through the pane's own copy.
func (f *fixture) reread(id model.ID) model.Issue {
	f.t.Helper()
	issue, err := f.core.ViewIssue(f.t.Context(), id)
	if err != nil {
		f.t.Fatalf("reread %s: %v", id, err)
	}
	return issue.Issue
}

// comments reads one issue's thread back out of the store.
func (f *fixture) comments(id model.ID) []model.Comment {
	f.t.Helper()
	issue, err := f.core.ViewIssue(f.t.Context(), id)
	if err != nil {
		f.t.Fatalf("reread %s: %v", id, err)
	}
	return issue.Comments
}

// model builds the navigator over this fixture, sized and loaded, exactly as
// Run would have left it before the program started.
func (f *fixture) model(width, height int) *Model {
	f.t.Helper()
	pane := New(f.core, f.project, "tester", f.editor, f.warnings)
	pane.ctx = f.t.Context()
	pane.width, pane.height = width, height
	pane.resize()
	if err := pane.reload(); err != nil {
		f.t.Fatalf("reload: %v", err)
	}
	return pane
}
