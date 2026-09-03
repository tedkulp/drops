package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/store"
)

// TestListRowByteExact pins the scanning-verb row the doc draws: one glyph, the
// id, the priority, the type padded to eight columns, then the title. Every
// byte, because a row is what a human scans and the doc reproduces it verbatim.
func TestListRowByteExact(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "create", "Build the render package", "--inbox", "-t", "decision", "-p", "0")

	out, _, code := run(t, db, cwd, "list", "--inbox")
	if code != 0 {
		t.Fatalf("list exit = %d", code)
	}
	want := fmt.Sprintf("○ %s P0 decision Build the render package\n", id)
	if out != want {
		t.Fatalf("list row = %q, want %q", out, want)
	}
}

// TestScanningRowIDColumnSizesToTheWidestID is why the doc tells you not to
// write a parser against a byte offset: the id column is as wide as the widest
// id in THIS result set, so column positions move between invocations.
func TestScanningRowIDColumnSizesToTheWidestID(t *testing.T) {
	db, cwd := newStore(t)
	short := mustRun(t, db, cwd, "q", "--inbox", "short id")
	parent := mustRun(t, db, cwd, "q", "--inbox", "a parent")
	long := mustRun(t, db, cwd, "create", "long id", "--parent", parent)

	narrow := mustRun(t, db, cwd, "list", "--inbox", "--parent", parent)
	if !strings.Contains(narrow, long+" P2") {
		t.Fatalf("with one id in the set the column is that id's width: %q", narrow)
	}

	wide := mustRun(t, db, cwd, "list", "--inbox")
	pad := strings.Repeat(" ", len(long)-len(short))
	if !strings.Contains(wide, short+pad+" P2") {
		t.Fatalf("the short id was not padded out to the widest id in the set:\n%s", wide)
	}
	if !strings.Contains(wide, long+" P2") {
		t.Fatalf("the widest id gained padding of its own:\n%s", wide)
	}
	_ = parent
}

// TestScanningVerbsShareOneOrder is the promise that makes a queue usable:
// priority ascending, then newest first, then by id, and relevance never
// reorders it — so search and list put the same two issues in the same order.
func TestScanningVerbsShareOneOrder(t *testing.T) {
	db, cwd := newStore(t)
	low := mustRun(t, db, cwd, "create", "shared term backlog", "--inbox", "-p", "4")
	high := mustRun(t, db, cwd, "create", "shared term critical", "--inbox", "-p", "0")
	mid := mustRun(t, db, cwd, "create", "shared term ordinary", "--inbox", "-p", "2")

	for _, verb := range [][]string{
		{"list", "--inbox"},
		{"search", "shared", "--inbox"},
		{"ready", "--inbox"},
	} {
		out := mustRun(t, db, cwd, verb...)
		gotHigh, gotMid, gotLow := strings.Index(out, high), strings.Index(out, mid), strings.Index(out, low)
		if gotHigh < 0 || gotMid < 0 || gotLow < 0 {
			t.Fatalf("%v dropped a row:\n%s", verb, out)
		}
		if !(gotHigh < gotMid && gotMid < gotLow) {
			t.Errorf("%v ordered P0/P2/P4 as %d/%d/%d:\n%s", verb, gotHigh, gotMid, gotLow, out)
		}
	}
}

// TestReadyContinuationIsIndentedFour and TestBlockedRowNamesItsBlockers are the
// two continuation lines the doc specifies: a fixed four columns in, and the
// blocker list comma-joined.
func TestReadyContinuationIsIndentedFour(t *testing.T) {
	db, cwd := newStore(t)
	blocker := mustRun(t, db, cwd, "q", "--inbox", "the blocker")
	first := mustRun(t, db, cwd, "q", "--inbox", "waits on it")
	second := mustRun(t, db, cwd, "q", "--inbox", "also waits on it")
	mustRun(t, db, cwd, "dep", "add", first, blocker)
	mustRun(t, db, cwd, "dep", "add", second, blocker)

	out, _, _ := run(t, db, cwd, "ready", "--inbox")
	if !strings.Contains(out, "\n    unblocks 2\n") {
		t.Fatalf("ready continuation is not `unblocks 2` indented four columns:\n%q", out)
	}
	if strings.Contains(out, first) || strings.Contains(out, second) {
		t.Fatalf("ready listed an issue with an open blocker:\n%s", out)
	}
}

func TestBlockedRowNamesItsBlockers(t *testing.T) {
	db, cwd := newStore(t)
	one := mustRun(t, db, cwd, "q", "--inbox", "blocker one")
	two := mustRun(t, db, cwd, "q", "--inbox", "blocker two")
	waiting := mustRun(t, db, cwd, "q", "--inbox", "the waiting issue")
	mustRun(t, db, cwd, "dep", "add", waiting, one)
	mustRun(t, db, cwd, "dep", "add", waiting, two)

	// The doc fixes the shape — four columns in, comma-joined — and says
	// nothing about which blocker leads, so neither does this.
	out, _, _ := run(t, db, cwd, "blocked", "--inbox")
	if !strings.Contains(out, fmt.Sprintf("\n    blocked by %s, %s\n", one, two)) &&
		!strings.Contains(out, fmt.Sprintf("\n    blocked by %s, %s\n", two, one)) {
		t.Fatalf("blocked continuation is not the comma-joined blocker list indented four:\n%q", out)
	}

	// The --json shape the doc warns a parser about: each element is
	// {issue, blocked_by}, and blocked_by is a bare array of id STRINGS.
	type element struct {
		Issue struct {
			ID string `json:"id"`
		} `json:"issue"`
		BlockedBy []string `json:"blocked_by"`
	}
	got := decodeMany[element](t, mustRun(t, db, cwd, "blocked", "--inbox", "--json"))
	if len(got) != 1 || got[0].Issue.ID != waiting {
		t.Fatalf("blocked --json = %#v, want one element wrapping %s", got, waiting)
	}
	if joined := strings.Join(got[0].BlockedBy, ","); joined != one+","+two && joined != two+","+one {
		t.Fatalf("blocked_by = %#v, want the two blocker ids as strings", got[0].BlockedBy)
	}
}

// TestReadyAndBlockedRefuseTheAllFlag: -a is meaningless on a queue whose
// members are open by definition, and a silently-ignored flag is worse than a
// refusal because it reads as an answer.
func TestReadyAndBlockedRefuseTheAllFlag(t *testing.T) {
	db, cwd := newStore(t)
	for _, verb := range []string{"ready", "blocked"} {
		if _, _, code := run(t, db, cwd, verb, "--inbox", "-a"); code != 2 {
			t.Errorf("%s -a exit = %d, want 2", verb, code)
		}
	}
}

// TestScanningVerbAdvisesAndExitsZeroWhenNothingResolves is the rough edge the
// doc tells a session to expect: check stdout, not the exit code.
func TestScanningVerbAdvisesAndExitsZeroWhenNothingResolves(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "q", "--inbox", "somewhere else")

	for _, verb := range [][]string{{"list"}, {"ready"}, {"blocked"}, {"search", "somewhere"}, {"count"}} {
		out, errOut, code := run(t, db, cwd, verb...)
		if code != 0 {
			t.Errorf("%v exit = %d, want 0", verb, code)
		}
		if !strings.Contains(errOut, "no project resolves here") {
			t.Errorf("%v printed no advisory on stderr: %q", verb, errOut)
		}
		if verb[0] == "count" {
			if strings.TrimSpace(out) != "0" {
				t.Errorf("count with nothing resolved = %q, want 0", out)
			}
			continue
		}
		if out != "" {
			t.Errorf("%v wrote %q to stdout with nothing resolved", verb, out)
		}
	}
}

// TestScanningVerbJSONIsAnEmptyArrayNotNull: an empty result is [], never null,
// so `jq '.[]'` on it is safe rather than an error.
func TestScanningVerbJSONIsAnEmptyArrayNotNull(t *testing.T) {
	db, cwd := newStore(t)
	for _, verb := range [][]string{
		{"list", "--inbox"}, {"ready", "--inbox"}, {"blocked", "--inbox"},
		{"search", "nothing", "--inbox"}, {"list"}, {"blocked"},
	} {
		out, _, code := run(t, db, cwd, append(verb, "--json")...)
		if code != 0 {
			t.Errorf("%v exit = %d", verb, code)
		}
		if out != "[]\n" {
			t.Errorf("%v --json on an empty result = %q, want \"[]\\n\"", verb, out)
		}
	}
}

// TestAllProjectsAddsTheProjectColumn: the column has to exist because move
// keeps an id byte-identical across a project change, so a prefix cannot answer
// which project a row is in.
func TestAllProjectsAddsTheProjectColumn(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "alpha")
	id := mustRun(t, db, cwd, "create", "over here", "-P", "alpha")

	scoped := mustRun(t, db, cwd, "list", "-P", "alpha")
	if strings.Contains(scoped, "alpha over here") {
		t.Fatalf("a project column appeared without --all-projects: %q", scoped)
	}
	every := mustRun(t, db, cwd, "list", "--all-projects")
	if !strings.Contains(every, id+" P2 task     alpha over here") {
		t.Fatalf("--all-projects row = %q, want a project column before the title", every)
	}
}

// TestListParentTakesDirectChildrenOnly is the documented rough edge: it
// matches on the "<parent>.N" id, so a further "." in the tail is a grandchild
// and is excluded.
func TestListParentTakesDirectChildrenOnly(t *testing.T) {
	db, cwd := newStore(t)
	parent := mustRun(t, db, cwd, "q", "--inbox", "the parent")
	child := mustRun(t, db, cwd, "create", "the child", "--parent", parent)
	grandchild := mustRun(t, db, cwd, "create", "the grandchild", "--parent", child)

	out := mustRun(t, db, cwd, "list", "--inbox", "--parent", parent)
	if !strings.Contains(out, child) {
		t.Fatalf("--parent dropped the direct child:\n%s", out)
	}
	if strings.Contains(out, grandchild) {
		t.Fatalf("--parent included the grandchild %s:\n%s", grandchild, out)
	}
	// An empty value would silently list every issue, which reads as an
	// answer rather than a mistake.
	if _, _, code := run(t, db, cwd, "list", "--inbox", "--parent", ""); code != 2 {
		t.Fatalf("list --parent \"\" exit = %d, want 2", code)
	}
}

// TestListLabelFiltersDemandAllAndAny: -l is every one of these labels,
// --label-any is at least one. Getting them the same way round is the whole
// difference between a filter and a wider listing.
func TestListLabelFiltersDemandAllAndAny(t *testing.T) {
	db, cwd := newStore(t)
	both := mustRun(t, db, cwd, "create", "carries both", "--inbox", "-l", "red", "-l", "blue")
	one := mustRun(t, db, cwd, "create", "carries one", "--inbox", "-l", "red")

	all := mustRun(t, db, cwd, "list", "--inbox", "-l", "red", "-l", "blue")
	if !strings.Contains(all, both) || strings.Contains(all, one) {
		t.Fatalf("-l red -l blue must demand both:\n%s", all)
	}
	any := mustRun(t, db, cwd, "list", "--inbox", "--label-any", "red,blue")
	if !strings.Contains(any, both) || !strings.Contains(any, one) {
		t.Fatalf("--label-any must accept either:\n%s", any)
	}
}

// TestListDeferredComparesAgainstNow is the clause the flag's help spells out:
// deferred INTO THE FUTURE, not merely non-null. Nothing writes deferred_until
// — the data is preserved from the old store — so the fixture writes it the way
// cutover leaves it.
func TestListDeferredComparesAgainstNow(t *testing.T) {
	db, cwd := newStore(t)
	future := mustRun(t, db, cwd, "q", "--inbox", "deferred into the future")
	past := mustRun(t, db, cwd, "q", "--inbox", "deferred in the past")
	plain := mustRun(t, db, cwd, "q", "--inbox", "never deferred")

	setDeferral(t, db, future, time.Now().Add(72*time.Hour))
	setDeferral(t, db, past, time.Now().Add(-72*time.Hour))

	out := mustRun(t, db, cwd, "list", "--inbox", "--deferred")
	if !strings.Contains(out, future) {
		t.Errorf("--deferred dropped the future deferral:\n%s", out)
	}
	if strings.Contains(out, past) {
		t.Errorf("--deferred kept an ELAPSED deferral, so it is testing non-null:\n%s", out)
	}
	if strings.Contains(out, plain) {
		t.Errorf("--deferred kept an issue with no deferral at all:\n%s", out)
	}
}

// setDeferral writes deferred_until straight into the store. No verb sets one,
// so this is the only way to exercise the read path that displays it.
func setDeferral(t *testing.T, dbPath, id string, when time.Time) {
	t.Helper()
	opened, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer opened.Close()
	if _, err := opened.DB().ExecContext(context.Background(),
		"UPDATE issues SET deferred_until = ? WHERE id = ?",
		when.UTC().Format(time.RFC3339Nano), id); err != nil {
		t.Fatalf("set deferred_until: %v", err)
	}
}

// TestCountAnswersTheScopeAndFilters: count is list's cardinality, so it has to
// mean the same by scope, status and type or it answers a different question
// from the listing beside it.
func TestCountAnswersTheScopeAndFilters(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "alpha")
	mustRun(t, db, cwd, "create", "a bug", "-P", "alpha", "-t", "bug")
	mustRun(t, db, cwd, "create", "a task", "-P", "alpha")
	closed := mustRun(t, db, cwd, "create", "done", "-P", "alpha")
	mustRun(t, db, cwd, "close", closed)
	mustRun(t, db, cwd, "q", "--inbox", "elsewhere")

	for _, testcase := range []struct {
		args []string
		want string
	}{
		{[]string{"count", "-P", "alpha"}, "2"},
		{[]string{"count", "-P", "alpha", "-a"}, "3"},
		{[]string{"count", "-P", "alpha", "-t", "bug"}, "1"},
		{[]string{"count", "-P", "alpha", "-s", "closed"}, "1"},
		{[]string{"count", "--inbox"}, "1"},
		{[]string{"count", "--all-projects"}, "3"},
		{[]string{"count", "--all-projects", "-a"}, "4"},
	} {
		if got := mustRun(t, db, cwd, testcase.args...); got != testcase.want {
			t.Errorf("%v = %q, want %q", testcase.args, got, testcase.want)
		}
	}
	if got := decodeOne[map[string]int](t, mustRun(t, db, cwd, "count", "-P", "alpha", "--json")); got["count"] != 2 {
		t.Errorf("count --json = %#v, want {\"count\":2}", got)
	}
}

// TestStatusFlagAllReachesTombstones: "" means the active statuses, -a adds the
// terminal one, and the literal "all" is the only form that reveals tombstones.
func TestStatusFlagAllReachesTombstones(t *testing.T) {
	db, cwd := newStore(t)
	live := mustRun(t, db, cwd, "q", "--inbox", "still here")
	gone := mustRun(t, db, cwd, "q", "--inbox", "removed")
	tombstone(t, db, gone)

	if out := mustRun(t, db, cwd, "list", "--inbox", "-a"); strings.Contains(out, gone) {
		t.Errorf("-a revealed a tombstone; only -s all does:\n%s", out)
	}
	out := mustRun(t, db, cwd, "list", "--inbox", "-s", "all")
	if !strings.Contains(out, gone) || !strings.Contains(out, live) {
		t.Fatalf("-s all did not span every status including tombstones:\n%s", out)
	}
	if !strings.Contains(out, "⊘ "+gone) {
		t.Errorf("a tombstoned row did not carry the ⊘ glyph:\n%s", out)
	}
	if _, _, code := run(t, db, cwd, "list", "--inbox", "-s", "nonsense"); code != 2 {
		t.Error("an unknown --status was not refused at 2")
	}
}

// dropFTSTrigger removes the trigger that keeps issues_fts in step with its
// source table, which is how a test produces the one desynchronisation the
// index check exists to catch.
func dropFTSTrigger(t *testing.T, dbPath string) {
	t.Helper()
	opened, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer opened.Close()
	if _, err := opened.DB().ExecContext(context.Background(), "DROP TRIGGER issues_fts_au"); err != nil {
		t.Fatalf("drop FTS update trigger: %v", err)
	}
}

func tombstone(t *testing.T, dbPath, id string) {
	t.Helper()
	opened, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer opened.Close()
	if _, err := opened.DB().ExecContext(context.Background(),
		"UPDATE issues SET tombstoned = 1 WHERE id = ?", id); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
}

// TestSearchCoversCommentBodies: a match in a comment is deliberately
// indistinguishable from a match in a description, so nothing became less
// findable when design/notes/acceptance_criteria folded into the thread.
func TestSearchCoversCommentBodies(t *testing.T) {
	db, cwd := newStore(t)
	byBody := mustRun(t, db, cwd, "create", "plain title", "--inbox", "-d", "a distinctive needle in the body")
	byComment := mustRun(t, db, cwd, "q", "--inbox", "another plain title")
	mustRun(t, db, cwd, "comment", "add", byComment, "a distinctive needle in a remark", "--author", "agent")
	missing := mustRun(t, db, cwd, "q", "--inbox", "unrelated")

	out := mustRun(t, db, cwd, "search", "needle", "--inbox")
	if !strings.Contains(out, byBody) || !strings.Contains(out, byComment) {
		t.Fatalf("search missed a description or a comment match:\n%s", out)
	}
	if strings.Contains(out, missing) {
		t.Fatalf("search matched an unrelated issue:\n%s", out)
	}
	if got := len(strings.Split(out, "\n")); got != 2 {
		t.Fatalf("search returned %d rows, want one per matching issue:\n%s", got, out)
	}
}

// TestScanningVerbTruncatesNothingOffATerminal is the rule that keeps
// `drops list | grep` lossless: truncation is gated on terminal-ness alone, and
// a test buffer is never a terminal.
func TestScanningVerbTruncatesNothingOffATerminal(t *testing.T) {
	db, cwd := newStore(t)
	title := "a title deliberately longer than any sane terminal width so that a truncating " +
		"renderer would certainly have to cut it somewhere before the end of this sentence"
	mustRun(t, db, cwd, "q", "--inbox", title)

	out := mustRun(t, db, cwd, "list", "--inbox")
	if !strings.Contains(out, title) {
		t.Fatalf("a long title did not survive off a terminal:\n%s", out)
	}
	if strings.Contains(out, "…") {
		t.Fatalf("something truncated off a terminal:\n%s", out)
	}
}

// TestLimitCapsTheResult: --limit is applied after every filter, so it caps
// what you would have seen rather than what the store looked at.
func TestLimitCapsTheResult(t *testing.T) {
	db, cwd := newStore(t)
	for i := 0; i < 5; i++ {
		mustRun(t, db, cwd, "q", "--inbox", "an issue")
	}
	for _, verb := range []string{"list", "ready"} {
		full := mustRun(t, db, cwd, verb, "--inbox")
		if got := len(strings.Split(full, "\n")); got != 5 {
			t.Fatalf("%s returned %d rows before limiting", verb, got)
		}
		capped := mustRun(t, db, cwd, verb, "--inbox", "--limit", "2")
		if got := len(strings.Split(capped, "\n")); got != 2 {
			t.Errorf("%s --limit 2 returned %d rows:\n%s", verb, got, capped)
		}
		if unlimited := mustRun(t, db, cwd, verb, "--inbox", "--limit", "0"); unlimited != full {
			t.Errorf("%s --limit 0 is not unlimited", verb)
		}
	}
}

// TestScanningVerbsTakeNoPositional: list, ready, blocked and count answer the
// scope, so a positional there is a mistyped flag rather than a filter.
func TestScanningVerbsTakeNoPositional(t *testing.T) {
	db, cwd := newStore(t)
	for _, verb := range []string{"list", "ready", "blocked", "count"} {
		if _, _, code := run(t, db, cwd, verb, "--inbox", "stray"); code != 2 {
			t.Errorf("%s with a positional exit = %d, want 2", verb, code)
		}
	}
}

// TestPageMarksATombstonedChildAndDoesNotCountItOpen is the documented split:
// `Children  N, M open`, where a tombstoned child is NOT open whatever its
// status column says. It is still listed — a page withholds no relation — and
// the ⊘ outranks the status glyph so a removed child cannot read as open.
func TestPageMarksATombstonedChildAndDoesNotCountItOpen(t *testing.T) {
	db, cwd := newStore(t)
	parent := mustRun(t, db, cwd, "q", "--inbox", "the parent")
	live := mustRun(t, db, cwd, "create", "a live child", "--parent", parent)
	dead := mustRun(t, db, cwd, "create", "a removed child", "--parent", parent)
	tombstone(t, db, dead)

	page := mustRun(t, db, cwd, "show", parent)
	if !strings.Contains(page, "○ "+live) {
		t.Fatalf("the live child is not marked open:\n%s", page)
	}
	if !strings.Contains(page, "⊘ "+dead) {
		t.Fatalf("the tombstoned child is missing or unmarked:\n%s", page)
	}
	if !strings.Contains(page, "Children  2, 1 open") {
		t.Fatalf("the children count counts the tombstone as open:\n%s", page)
	}
}

// TestPageDropsAWithdrawnDependency is the other tombstone on a page, and a
// different record: `dep rm` retires the EDGE, so the relation goes rather than
// being marked. An issue is still there; the claim that it blocks this one is
// not.
func TestPageDropsAWithdrawnDependency(t *testing.T) {
	db, cwd := newStore(t)
	blocked := mustRun(t, db, cwd, "q", "--inbox", "the blocked issue")
	kept := mustRun(t, db, cwd, "q", "--inbox", "a standing blocker")
	withdrawn := mustRun(t, db, cwd, "q", "--inbox", "a withdrawn blocker")
	mustRun(t, db, cwd, "dep", "add", blocked, kept)
	mustRun(t, db, cwd, "dep", "add", blocked, withdrawn)
	mustRun(t, db, cwd, "dep", "rm", blocked, withdrawn)

	page := mustRun(t, db, cwd, "show", blocked)
	if !strings.Contains(page, kept) {
		t.Fatalf("the standing blocker left the page:\n%s", page)
	}
	if strings.Contains(page, withdrawn) {
		t.Fatalf("a withdrawn dependency is still named on the page:\n%s", page)
	}
	view := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", blocked, "--json"))
	if got := view["blockers"].([]any); len(got) != 1 {
		t.Fatalf("blockers in --json = %#v, want only the standing one", got)
	}
	// The withdrawn blocker's own page loses the other end of the same edge.
	if other := mustRun(t, db, cwd, "show", withdrawn); strings.Contains(other, "Blocks") {
		t.Fatalf("the withdrawn edge survives from the other end:\n%s", other)
	}
}
