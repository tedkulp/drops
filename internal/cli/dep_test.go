package cli

import (
	"strings"
	"testing"
)

// TestDepAddMakesTheBlockedIssueUnready is dep add's store effect stated as the
// thing that depends on it: `drops dep add <id> <blocker-id>` means <blocker-id>
// blocks <id>, so <id> leaves ready and appears in blocked. Getting the
// argument order backwards is silent otherwise — both ids exist, both rows
// render, and only the frontier is wrong.
func TestDepAddMakesTheBlockedIssueUnready(t *testing.T) {
	db, cwd := newStore(t)
	blocked := mustRun(t, db, cwd, "q", "--inbox", "the blocked issue")
	blocker := mustRun(t, db, cwd, "q", "--inbox", "the blocker")

	if got := mustRun(t, db, cwd, "dep", "add", blocked, blocker); got != blocked+" now depends on "+blocker+" (blocks)" {
		t.Fatalf("dep add printed %q", got)
	}

	ready := mustRun(t, db, cwd, "ready", "--inbox")
	if strings.Contains(ready, blocked) {
		t.Fatalf("the blocked issue is still ready:\n%s", ready)
	}
	if !strings.Contains(ready, blocker) {
		t.Fatalf("the blocker left the frontier:\n%s", ready)
	}
	if out := mustRun(t, db, cwd, "blocked", "--inbox"); !strings.Contains(out, blocked) {
		t.Fatalf("the blocked issue is not in blocked:\n%s", out)
	}

	// And it reads back from both ends of the edge on the page.
	if page := mustRun(t, db, cwd, "show", blocked); !strings.Contains(page, "Blocked by") {
		t.Fatalf("the blocked issue's page names no blocker:\n%s", page)
	}
	if page := mustRun(t, db, cwd, "show", blocker); !strings.Contains(page, "Blocks") {
		t.Fatalf("the blocker's page names nothing it blocks:\n%s", page)
	}
}

// TestDepRmRestoresReadiness is the same edge withdrawn. rm takes the type it
// was added with, so removing a `blocks` edge as `related` must not silently
// count as done.
func TestDepRmRestoresReadiness(t *testing.T) {
	db, cwd := newStore(t)
	blocked := mustRun(t, db, cwd, "q", "--inbox", "the blocked issue")
	blocker := mustRun(t, db, cwd, "q", "--inbox", "the blocker")
	mustRun(t, db, cwd, "dep", "add", blocked, blocker)

	// rm names the type it withdraws, so asking for an edge that is not
	// there is a not-found refusal rather than a success that leaves the
	// blocks edge standing while reporting it gone.
	if _, _, code := run(t, db, cwd, "dep", "rm", blocked, blocker, "--type", "related"); code != 4 {
		t.Fatalf("dep rm of an absent `related` edge exit = %d, want 4", code)
	}
	if out := mustRun(t, db, cwd, "ready", "--inbox"); strings.Contains(out, blocked) {
		t.Fatalf("removing a `related` edge unblocked a `blocks` one:\n%s", out)
	}

	if got := mustRun(t, db, cwd, "dep", "rm", blocked, blocker); got != "removed "+blocked+" -> "+blocker+" (blocks)" {
		t.Fatalf("dep rm printed %q", got)
	}
	if out := mustRun(t, db, cwd, "ready", "--inbox"); !strings.Contains(out, blocked) {
		t.Fatalf("the issue is still blocked after its only blocker was removed:\n%s", out)
	}
}

// TestDepAddRelatedIsUndirectedFromEitherEnd is the store effect of the one
// undirected type, stated as what a reader observes: `related` spelled from
// either end is ONE relation, listed once on both pages, and `dep rm` from
// either end withdraws it. Adding the reciprocal used to store a second row,
// which `show` then rendered and serialized twice (y7f6z).
func TestDepAddRelatedIsUndirectedFromEitherEnd(t *testing.T) {
	db, cwd := newStore(t)
	first := mustRun(t, db, cwd, "q", "--inbox", "the first issue")
	second := mustRun(t, db, cwd, "q", "--inbox", "the second issue")

	mustRun(t, db, cwd, "dep", "add", first, second, "--type", "related")
	mustRun(t, db, cwd, "dep", "add", second, first, "--type", "related")

	for _, end := range [][2]string{{first, second}, {second, first}} {
		related := decodeRelatedIDs(t, mustRun(t, db, cwd, "show", end[0], "--json"))
		if len(related) != 1 || related[0] != end[1] {
			t.Fatalf("%s .related = %v, want [%s] once", end[0], related, end[1])
		}
		page := mustRun(t, db, cwd, "show", end[0])
		rows := 0
		for _, line := range strings.Split(page, "\n") {
			if strings.Contains(line, end[1]) {
				rows++
			}
		}
		if rows != 1 {
			t.Fatalf("%s names %s on %d lines, want the one relation row:\n%s", end[0], end[1], rows, page)
		}
	}

	// And the relation comes off from the end that did not store it.
	if got := mustRun(t, db, cwd, "dep", "rm", second, first, "--type", "related"); got != "removed "+second+" -> "+first+" (related)" {
		t.Fatalf("dep rm printed %q", got)
	}
	if related := decodeRelatedIDs(t, mustRun(t, db, cwd, "show", first, "--json")); len(related) != 0 {
		t.Fatalf("%s .related = %v after the relation was removed", first, related)
	}
}

// decodeRelatedIDs reads `show --json`'s `.related` ids, which is the shape an
// agent parses the relation out of.
func decodeRelatedIDs(t *testing.T, out string) []string {
	t.Helper()
	page := decodeOne[struct {
		Related []struct {
			ID string `json:"id"`
		} `json:"related"`
	}](t, out)
	ids := make([]string, 0, len(page.Related))
	for _, ref := range page.Related {
		ids = append(ids, ref.ID)
	}
	return ids
}

// TestDepTypeMustBeOneOfThree: an unknown type is a refusal at 2, not a stored
// edge nothing will ever match.
func TestDepTypeMustBeOneOfThree(t *testing.T) {
	db, cwd := newStore(t)
	first := mustRun(t, db, cwd, "q", "--inbox", "first")
	second := mustRun(t, db, cwd, "q", "--inbox", "second")

	for _, valid := range []string{"blocks", "related", "discovered-from"} {
		if _, _, code := run(t, db, cwd, "dep", "add", first, second, "--type", valid); code != 0 {
			t.Errorf("dep add --type %s exit = %d, want 0", valid, code)
		}
	}
	// The CLI names the three valid spellings, which is what makes the
	// refusal actionable — core's own rejection knows the value is wrong and
	// not what would have been right.
	for _, verb := range []string{"add", "rm"} {
		_, _, err := RunForTest([]string{"dep", verb, first, second, "--type", "sideways"}, db, cwd)
		if ExitCodeFor(err) != 2 {
			t.Errorf("dep %s --type sideways exit = %d, want 2", verb, ExitCodeFor(err))
			continue
		}
		if !strings.Contains(err.Error(), "blocks|related|discovered-from") {
			t.Errorf("dep %s --type sideways said %q, want the three valid types named", verb, err)
		}
	}
}

// TestDepTreePrintsTheChainIndented: the longest chain of open blockers,
// deepest last, two columns per level.
func TestDepTreePrintsTheChainIndented(t *testing.T) {
	db, cwd := newStore(t)
	top := mustRun(t, db, cwd, "q", "--inbox", "the top")
	middle := mustRun(t, db, cwd, "q", "--inbox", "the middle")
	bottom := mustRun(t, db, cwd, "q", "--inbox", "the bottom")
	shallow := mustRun(t, db, cwd, "q", "--inbox", "a shallower blocker")
	mustRun(t, db, cwd, "dep", "add", top, middle)
	mustRun(t, db, cwd, "dep", "add", top, shallow)
	mustRun(t, db, cwd, "dep", "add", middle, bottom)

	out, _, code := run(t, db, cwd, "dep", "tree", top)
	if code != 0 {
		t.Fatalf("dep tree exit = %d", code)
	}
	want := top + "\n  " + middle + "\n    " + bottom + "\n"
	if out != want {
		t.Fatalf("dep tree = %q, want %q (the DEEPEST chain, two columns per level)", out, want)
	}
	if got := decodeMany[string](t, mustRun(t, db, cwd, "dep", "tree", top, "--json")); strings.Join(got, ",") != top+","+middle+","+bottom {
		t.Fatalf("dep tree --json = %#v, want the chain as a bare array", got)
	}
}

// TestDepCyclesReportsAndExitsOne: dep add deliberately does not refuse a cycle
// — it records the edge and leaves cycles to find it — so a map whose edges were
// wired in a second pass is worth running this against. Exit 1 here is a report,
// not a crash, which is why the doc calls it out as an overload.
func TestDepCyclesReportsAndExitsOne(t *testing.T) {
	db, cwd := newStore(t)
	first := mustRun(t, db, cwd, "q", "--inbox", "first")
	second := mustRun(t, db, cwd, "q", "--inbox", "second")

	out, _, code := run(t, db, cwd, "dep", "cycles")
	if code != 0 || out != "" {
		t.Fatalf("dep cycles on a sound store = %q at exit %d, want silence at 0", out, code)
	}
	if got := mustRun(t, db, cwd, "dep", "cycles", "--json"); got != "[]" {
		t.Fatalf("dep cycles --json with no cycles = %q, want []", got)
	}

	mustRun(t, db, cwd, "dep", "add", first, second)
	if _, _, code := run(t, db, cwd, "dep", "add", second, first); code != 0 {
		t.Fatal("dep add refused the closing edge of a cycle; it is meant to record it")
	}

	out, _, code = run(t, db, cwd, "dep", "cycles")
	if code != 1 {
		t.Fatalf("dep cycles with a cycle exit = %d, want 1", code)
	}
	if !strings.Contains(out, first) || !strings.Contains(out, second) {
		t.Fatalf("dep cycles exited 1 without naming the cycle on stdout: %q", out)
	}
	// --json is the same report, so it exits 1 too: the finding is the
	// content, not the code.
	asJSON, _, jsonCode := run(t, db, cwd, "dep", "cycles", "--json")
	if jsonCode != 1 {
		t.Errorf("dep cycles --json with a cycle exit = %d, want 1", jsonCode)
	}
	if got := decodeMany[[]string](t, asJSON); len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("dep cycles --json = %#v, want one cycle of two ids", got)
	}
}
