package cli

import (
	"strings"
	"testing"
)

// moveJSON is the documented --json shape: one object, never an array, with the
// four lists always present.
type moveJSON struct {
	Moved []struct {
		ID         string `json:"id"`
		ProjectKey string `json:"project_key"`
	} `json:"moved"`
	NoOp []struct {
		ID string `json:"id"`
	} `json:"noop"`
	CrossingBlocks    []moveEdge `json:"crossing_blocks"`
	CrossingParentage []moveEdge `json:"crossing_parentage"`
	To                string     `json:"to"`
	Committed         bool       `json:"committed"`
}

// TestMoveKeepsTheIDAndChangesTheProject is move's whole store effect, and the
// warning the doc repeats twice: IDS ARE NEVER REWRITTEN.
func TestMoveKeepsTheIDAndChangesTheProject(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "from")
	mustRun(t, db, cwd, "project", "add", "--slug", "to")
	id := mustRun(t, db, cwd, "create", "refile me", "-P", "from")
	was := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))["project_key"]

	mustRun(t, db, cwd, "move", id, "--to", "to")

	view := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))
	if view["id"] != id {
		t.Fatalf("move rewrote the id to %v", view["id"])
	}
	if view["project_key"] == was {
		t.Fatalf("move left the project key at %v", was)
	}
	if out := mustRun(t, db, cwd, "list", "-P", "to"); !strings.Contains(out, id) {
		t.Fatalf("the issue is not in the destination:\n%s", out)
	}
	if out := mustRun(t, db, cwd, "list", "-P", "from"); strings.Contains(out, id) {
		t.Fatalf("the issue is still in the source:\n%s", out)
	}

	// The destination is --to, never -P: -P is the scoping flag on every
	// verb, so a move without --to must be refused rather than quietly
	// scoping the lookup and moving nothing.
	if _, _, code := run(t, db, cwd, "move", id, "-P", "from"); code != 2 {
		t.Error("move without --to was not refused at 2")
	}
	// The destination must already exist; move never auto-creates one.
	if _, _, code := run(t, db, cwd, "move", id, "--to", "nowhere"); code != 4 {
		t.Error("move to an unknown project was not a not-found refusal")
	}
}

// TestMoveReportsTheEdgesItMadeCross is the report a reader is told to act on:
// a blocker left behind makes an issue un-ready with no visible cause where it
// now lives. An edge whose two ends print the same project names no boundary at
// all, so the moved end reports where it is GOING.
func TestMoveReportsTheEdgesItMadeCross(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "from")
	mustRun(t, db, cwd, "project", "add", "--slug", "to")
	mustRun(t, db, cwd, "project", "add", "--slug", "third")
	moving := mustRun(t, db, cwd, "create", "the mover", "-P", "from")
	staying := mustRun(t, db, cwd, "create", "the blocker", "-P", "from")
	child := mustRun(t, db, cwd, "create", "the child", "--parent", moving)
	mustRun(t, db, cwd, "dep", "add", moving, staying)

	got := decodeOne[moveJSON](t, mustRun(t, db, cwd, "move", moving, "--to", "to", "--json"))
	if got.To != "to" || !got.Committed {
		t.Fatalf("move --json = %#v", got)
	}
	if len(got.Moved) != 1 || got.Moved[0].ID != moving {
		t.Fatalf("moved = %#v", got.Moved)
	}

	if len(got.CrossingBlocks) != 1 {
		t.Fatalf("crossing_blocks = %#v, want the blocker left behind", got.CrossingBlocks)
	}
	block := got.CrossingBlocks[0]
	if block.FromID != moving || block.ToID != staying {
		t.Fatalf("crossing block = %#v, want %s -> %s", block, moving, staying)
	}
	if block.FromProject != "to" || block.ToProject != "from" {
		t.Fatalf("crossing block projects = %q -> %q, want to -> from; "+
			"reading both ends out of the store prints the same project on both sides, "+
			"so a crossing that never crosses", block.FromProject, block.ToProject)
	}

	if len(got.CrossingParentage) != 1 {
		t.Fatalf("crossing_parentage = %#v, want the child left behind", got.CrossingParentage)
	}
	parent := got.CrossingParentage[0]
	if parent.FromID != child || parent.ToID != moving {
		t.Fatalf("crossing parentage = %#v, want %s -> %s", parent, child, moving)
	}
	if parent.FromProject != "from" || parent.ToProject != "to" {
		t.Fatalf("crossing parentage projects = %q -> %q, want from -> to", parent.FromProject, parent.ToProject)
	}

	// The text form says the same thing, naming both ends and both projects.
	// The child is still in `from` and its parent has gone, so this is the
	// parentage crossing read the other way round.
	text := mustRun(t, db, cwd, "move", child, "--to", "third")
	if !strings.Contains(text, "crossing parentage (1)") ||
		!strings.Contains(text, child+" (third) has parent "+moving+" (to)") {
		t.Fatalf("the text report does not name the edge and its two projects:\n%s", text)
	}

	// And an edge the move JOINS is not a crossing. Sending the blocker
	// after the mover puts both ends in `to`, so there is no boundary left
	// to report — one endpoint moving is not on its own a crossing.
	rejoined := decodeOne[moveJSON](t, mustRun(t, db, cwd, "move", staying, "--to", "to", "--json"))
	if len(rejoined.CrossingBlocks) != 0 {
		t.Fatalf("crossing_blocks = %#v after both ends landed in one project", rejoined.CrossingBlocks)
	}
}

// TestMoveSubtreeTakesEveryDescendant, and takes them transitively: a
// grandchild left behind is the whole reason --subtree exists.
func TestMoveSubtreeTakesEveryDescendant(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "from")
	mustRun(t, db, cwd, "project", "add", "--slug", "to")
	root := mustRun(t, db, cwd, "create", "the root", "-P", "from")
	child := mustRun(t, db, cwd, "create", "the child", "--parent", root)
	grandchild := mustRun(t, db, cwd, "create", "the grandchild", "--parent", child)
	unrelated := mustRun(t, db, cwd, "create", "unrelated", "-P", "from")

	got := decodeOne[moveJSON](t, mustRun(t, db, cwd, "move", root, "--to", "to", "--subtree", "--json"))
	moved := map[string]bool{}
	for _, m := range got.Moved {
		moved[m.ID] = true
	}
	for _, want := range []string{root, child, grandchild} {
		if !moved[want] {
			t.Errorf("--subtree left %s behind: %#v", want, got.Moved)
		}
	}
	if moved[unrelated] {
		t.Error("--subtree took an unrelated issue")
	}
	// Nothing crossed: the whole subtree went together.
	if len(got.CrossingParentage) != 0 {
		t.Errorf("crossing_parentage = %#v, want none when the whole subtree moves", got.CrossingParentage)
	}
	if out := mustRun(t, db, cwd, "list", "-P", "from"); !strings.Contains(out, unrelated) || strings.Contains(out, grandchild) {
		t.Errorf("source project after --subtree:\n%s", out)
	}
}

// TestMoveReportsANoOpRatherThanFailing: an issue already in the destination is
// a reported no-op, so re-running a half-finished triage is safe.
func TestMoveReportsANoOpRatherThanFailing(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "to")
	already := mustRun(t, db, cwd, "create", "already there", "-P", "to")
	fresh := mustRun(t, db, cwd, "q", "--inbox", "not yet")

	out, _, code := run(t, db, cwd, "move", already, fresh, "--to", "to", "--json")
	if code != 0 {
		t.Fatalf("re-running a half-finished move exit = %d, want 0", code)
	}
	got := decodeOne[moveJSON](t, out)
	if len(got.NoOp) != 1 || got.NoOp[0].ID != already {
		t.Fatalf("noop = %#v, want %s", got.NoOp, already)
	}
	if len(got.Moved) != 1 || got.Moved[0].ID != fresh {
		t.Fatalf("moved = %#v, want %s", got.Moved, fresh)
	}
	// The four lists are always present, so a parser can index them.
	empty := mustRun(t, db, cwd, "move", already, "--to", "to", "--json")
	for _, key := range []string{"\"moved\":[]", "\"noop\":[", "\"crossing_blocks\":[]", "\"crossing_parentage\":[]"} {
		if !strings.Contains(empty, key) {
			t.Errorf("move --json is missing %s: %s", key, empty)
		}
	}
	if !strings.HasPrefix(empty, "{") {
		t.Errorf("move --json emitted %q, want ONE object rather than an array", empty)
	}
}

// TestMoveDeduplicatesItsIDs: naming an issue twice, or naming a parent and a
// child of it under --subtree, must move it once. A duplicate reaching the
// write would report the same issue twice and move it against itself.
func TestMoveDeduplicatesItsIDs(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "from")
	mustRun(t, db, cwd, "project", "add", "--slug", "to")
	root := mustRun(t, db, cwd, "create", "the root", "-P", "from")
	child := mustRun(t, db, cwd, "create", "the child", "--parent", root)

	got := decodeOne[moveJSON](t, mustRun(t, db, cwd, "move", root, root, child, "--to", "to", "--subtree", "--json"))
	seen := map[string]int{}
	for _, m := range got.Moved {
		seen[m.ID]++
	}
	if seen[root] != 1 || seen[child] != 1 || len(got.Moved) != 2 {
		t.Fatalf("moved = %#v, want each id exactly once", got.Moved)
	}
}

// TestMoveDryRunWritesNothing: it runs the real write path and rolls it back,
// printing what a real move would print.
func TestMoveDryRunWritesNothing(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "from")
	mustRun(t, db, cwd, "project", "add", "--slug", "to")
	id := mustRun(t, db, cwd, "create", "stay put", "-P", "from")

	out := mustRun(t, db, cwd, "move", id, "--to", "to", "--dry-run")
	if !strings.HasPrefix(out, "dry run: nothing was written") {
		t.Fatalf("--dry-run did not say so first:\n%s", out)
	}
	if !strings.Contains(out, "moved 1 issue to to") {
		t.Fatalf("--dry-run did not print what a real move would:\n%s", out)
	}
	if out := mustRun(t, db, cwd, "list", "-P", "from"); !strings.Contains(out, id) {
		t.Fatalf("--dry-run actually moved the issue:\n%s", out)
	}
	if out := mustRun(t, db, cwd, "list", "-P", "to"); strings.Contains(out, id) {
		t.Fatalf("--dry-run left the issue in the destination:\n%s", out)
	}
	if got := decodeOne[moveJSON](t, mustRun(t, db, cwd, "move", id, "--to", "to", "--dry-run", "--json")); got.Committed {
		t.Fatal("committed = true on a --dry-run")
	}
}

// TestMoveTakesEveryStatusIncludingTombstones: a triage that silently skipped
// closed or removed issues would leave a project half-refiled.
func TestMoveTakesEveryStatusIncludingTombstones(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "from")
	mustRun(t, db, cwd, "project", "add", "--slug", "to")
	open := mustRun(t, db, cwd, "create", "open", "-P", "from")
	closed := mustRun(t, db, cwd, "create", "closed", "-P", "from")
	removed := mustRun(t, db, cwd, "create", "removed", "-P", "from")
	mustRun(t, db, cwd, "close", closed)
	tombstone(t, db, removed)

	mustRun(t, db, cwd, "move", open, closed, removed, "--to", "to")
	out := mustRun(t, db, cwd, "list", "-P", "to", "-s", "all")
	for _, want := range []string{open, closed, removed} {
		if !strings.Contains(out, want) {
			t.Errorf("%s did not move:\n%s", want, out)
		}
	}
}
