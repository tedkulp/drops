package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestQuickCapturePrintsOnlyTheID pins the one thing the doc promises about q:
// "prints the id, nothing else". A capture verb whose output a shell substitutes
// straight into the next command cannot afford a second line.
func TestQuickCapturePrintsOnlyTheID(t *testing.T) {
	db, cwd := newStore(t)
	out, errOut, code := run(t, db, cwd, "q", "--inbox", "capture me")
	if code != 0 {
		t.Fatalf("q exit = %d (stderr: %s)", code, errOut)
	}
	id := strings.TrimSpace(out)
	if out != id+"\n" {
		t.Fatalf("q printed %q, want exactly the id and one newline", out)
	}
	if strings.ContainsAny(id, " \t") || id == "" {
		t.Fatalf("q printed %q, which is not a bare id", id)
	}
	// It really did create the issue it named, at the documented defaults.
	shown := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))
	if shown["title"] != "capture me" || shown["issue_type"] != "task" || shown["priority"] != float64(2) {
		t.Fatalf("q created %#v, want title/task/P2", shown)
	}
}

// TestCreateFilesUnderItsParentsProject is the rule dw32p's own map depends on:
// --parent carries the project and the working directory is not consulted, so a
// ticket lands in its map's project from any directory.
func TestCreateFilesUnderItsParentsProject(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "alpha")
	parent := mustRun(t, db, cwd, "create", "the map", "-P", "alpha", "-t", "epic")

	// cwd resolves to nothing at all, and -P is absent: only --parent can
	// answer where this goes.
	child := mustRun(t, db, cwd, "create", "a ticket", "--parent", parent)
	if want := parent + ".1"; child != want {
		t.Fatalf("first child id = %q, want %q (a child takes .N under its parent, from .1)", child, want)
	}

	parentJSON := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", parent, "--json"))
	childJSON := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", child, "--json"))
	if childJSON["project_key"] != parentJSON["project_key"] {
		t.Fatalf("child project_key = %v, parent's = %v; --parent must carry the project",
			childJSON["project_key"], parentJSON["project_key"])
	}

	// A -P that AGREES with --parent is fine: only a contradiction is an
	// error, so a caller who spells out where a ticket goes is not punished
	// for being explicit.
	if _, _, code := run(t, db, cwd, "create", "explicit", "--parent", parent, "-P", "alpha"); code != 0 {
		t.Fatalf("create --parent with an AGREEING -P exit = %d, want 0", code)
	}

	// And a --parent naming no issue is a refusal, not a mint: scanning for
	// "<parent>.%" alone used to put zzzzz.1 under an issue that never existed.
	if _, _, code := run(t, db, cwd, "create", "orphan", "--parent", "zzzzz"); code != 4 {
		t.Fatalf("create --parent zzzzz exit = %d, want 4", code)
	}
	if _, _, code := run(t, db, cwd, "show", "zzzzz.1", "--json"); code != 4 {
		t.Fatalf("a refused --parent minted zzzzz.1: show exit = %d, want 4 (not found)", code)
	}
}

// TestCreateFlagsLandOnTheIssue covers the detail form the doc advertises:
// -t, -p, -d, -l and -A all reach the stored record.
func TestCreateFlagsLandOnTheIssue(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "create", "detailed", "--inbox",
		"-t", "bug", "-p", "0", "-d", "the body", "-l", "one,two", "-A", "alice")

	var view struct {
		Title    string   `json:"title"`
		Type     string   `json:"issue_type"`
		Priority int      `json:"priority"`
		Body     string   `json:"description"`
		Assignee *string  `json:"assignee"`
		Labels   []string `json:"labels"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, db, cwd, "show", id, "--json")), &view); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if view.Title != "detailed" || view.Type != "bug" || view.Priority != 0 || view.Body != "the body" {
		t.Fatalf("create flags landed as %#v", view)
	}
	if view.Assignee == nil || *view.Assignee != "alice" {
		t.Fatalf("assignee = %v, want alice", view.Assignee)
	}
	if strings.Join(view.Labels, ",") != "one,two" {
		t.Fatalf("labels = %v, want [one two] (-l is comma-separated as well as repeatable)", view.Labels)
	}
}

// TestShowPageOrdersIdentityBodyRelationsComments asserts the page the doc
// draws: what the issue IS before what it is attached to.
func TestShowPageOrdersIdentityBodyRelationsComments(t *testing.T) {
	db, cwd := newStore(t)
	parent := mustRun(t, db, cwd, "create", "the parent", "--inbox", "-t", "epic", "-p", "1", "-l", "wayfinder:map")
	child := mustRun(t, db, cwd, "create", "the child", "--parent", parent)
	blocker := mustRun(t, db, cwd, "q", "--inbox", "the blocker")
	mustRun(t, db, cwd, "dep", "add", child, blocker)
	mustRun(t, db, cwd, "update", child, "-d", "The question this ticket resolves.")
	mustRun(t, db, cwd, "comment", "add", child, "a remark", "--author", "agent")

	page := mustRun(t, db, cwd, "show", child)
	order := []string{"the child", child + " · inbox · open · task · P2", "opened ",
		"The question this ticket resolves.", "Parent", "Blocked by", "Comments  1", "a remark"}
	at := 0
	for _, want := range order {
		next := strings.Index(page[at:], want)
		if next < 0 {
			t.Fatalf("page is missing %q, or has it out of order, after byte %d:\n%s", want, at, page)
		}
		at += next
	}
	if strings.Contains(page, "Children") {
		t.Errorf("an empty relation block printed its heading:\n%s", page)
	}
}

// A `related` edge is undirected to a reader, so show merges both stored
// directions under one heading. Outgoing edges stay first because core has
// already ordered each half of the graph.
func TestShowReadsRelatedEdgesFromEitherEnd(t *testing.T) {
	db, cwd := newStore(t)
	subject := mustRun(t, db, cwd, "q", "--inbox", "the subject")
	outgoing := mustRun(t, db, cwd, "q", "--inbox", "an outgoing peer")
	incoming := mustRun(t, db, cwd, "q", "--inbox", "an incoming peer")
	mustRun(t, db, cwd, "dep", "add", subject, outgoing, "--type", "related")
	mustRun(t, db, cwd, "dep", "add", incoming, subject, "--type", "related")

	page := mustRun(t, db, cwd, "show", subject)
	if !strings.Contains(page, "\nRelated\n") ||
		!strings.Contains(page, outgoing) || !strings.Contains(page, incoming) {
		t.Fatalf("related edges from both directions are not one page block:\n%s", page)
	}

	jsonView := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", subject, "--json"))
	related, ok := jsonView["related"].([]any)
	if !ok || len(related) != 2 {
		t.Fatalf("related in --json = %#v, want both directions", jsonView["related"])
	}
	got := []string{
		related[0].(map[string]any)["id"].(string),
		related[1].(map[string]any)["id"].(string),
	}
	if strings.Join(got, ",") != outgoing+","+incoming {
		t.Fatalf("related ids = %v, want outgoing then incoming", got)
	}
}

// A `discovered-from` edge names opposite facts at its two ends: an outgoing
// edge is where this issue came from; an incoming edge is what came out of it.
func TestShowNamesBothDiscoveredFromDirections(t *testing.T) {
	db, cwd := newStore(t)
	subject := mustRun(t, db, cwd, "q", "--inbox", "the subject")
	origin := mustRun(t, db, cwd, "q", "--inbox", "where it came from")
	derived := mustRun(t, db, cwd, "q", "--inbox", "what came out of it")
	mustRun(t, db, cwd, "dep", "add", subject, origin, "--type", "discovered-from")
	mustRun(t, db, cwd, "dep", "add", derived, subject, "--type", "discovered-from")

	page := mustRun(t, db, cwd, "show", subject)
	if !strings.Contains(page, "\nDiscovered from\n  ○ "+origin) {
		t.Fatalf("outgoing discovered-from edge is not named as the origin:\n%s", page)
	}
	if !strings.Contains(page, "\nDiscovered\n  ○ "+derived) {
		t.Fatalf("incoming discovered-from edge is not named as the derived issue:\n%s", page)
	}

	jsonView := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", subject, "--json"))
	for key, want := range map[string]string{
		"discovered_from": origin,
		"discovered":      derived,
	} {
		refs, ok := jsonView[key].([]any)
		if !ok || len(refs) != 1 || refs[0].(map[string]any)["id"] != want {
			t.Errorf("%s in --json = %#v, want %s", key, jsonView[key], want)
		}
	}
}

// TestShowSeveralIDsEmitsAnArrayAndRulesPagesApart is the shape warning the doc
// gives before you write a parser: one id is an object, several are an array.
func TestShowSeveralIDsEmitsAnArrayAndRulesPagesApart(t *testing.T) {
	db, cwd := newStore(t)
	first := mustRun(t, db, cwd, "q", "--inbox", "first")
	second := mustRun(t, db, cwd, "q", "--inbox", "second")

	one := mustRun(t, db, cwd, "show", first, "--json")
	if !strings.HasPrefix(one, "{") {
		t.Fatalf("show <id> --json = %q, want a bare object", one)
	}
	many := mustRun(t, db, cwd, "show", first, second, "--json")
	if !strings.HasPrefix(many, "[") {
		t.Fatalf("show <a> <b> --json = %q, want a bare array", many)
	}
	if got := len(decodeMany[map[string]any](t, many)); got != 2 {
		t.Fatalf("show <a> <b> --json had %d elements, want 2", got)
	}

	page := mustRun(t, db, cwd, "show", first, second)
	if strings.Count(page, strings.Repeat("─", 80)) != 1 {
		t.Fatalf("two pages were not ruled apart exactly once:\n%s", page)
	}
	if strings.Index(page, "first") > strings.Index(page, "second") {
		t.Fatalf("pages came back out of argument order:\n%s", page)
	}
}

// TestShowWrapsALongRelationTitleUnderTheIDColumn is the correction dw32p.20
// made to the reference build, asserted from the CLI where a reader meets it:
// a page truncates nothing, relation titles included.
func TestShowWrapsALongRelationTitleUnderTheIDColumn(t *testing.T) {
	db, cwd := newStore(t)
	long := "a relation title deliberately far longer than the eighty column budget a page wraps at"
	parent := mustRun(t, db, cwd, "q", "--inbox", "the parent")
	mustRun(t, db, cwd, "create", long, "--parent", parent)

	page := mustRun(t, db, cwd, "show", parent)
	if strings.Contains(page, "…") {
		t.Fatalf("show truncated a relation title:\n%s", page)
	}
	for _, word := range strings.Fields(long) {
		if !strings.Contains(page, word) {
			t.Fatalf("word %q was dropped from the wrapped relation title:\n%s", word, page)
		}
	}
}

// TestShowOrdersChildrenNaturally: ".2" precedes ".10". A wrong comparator here
// is invisible until a map's eleventh ticket, which is exactly when a reader
// stops trusting the listing.
func TestShowOrdersChildrenNaturally(t *testing.T) {
	db, cwd := newStore(t)
	parent := mustRun(t, db, cwd, "q", "--inbox", "the map")
	for i := 1; i <= 11; i++ {
		mustRun(t, db, cwd, "create", fmt.Sprintf("ticket %d", i), "--parent", parent)
	}
	page := mustRun(t, db, cwd, "show", parent)
	at := 0
	for i := 1; i <= 11; i++ {
		want := fmt.Sprintf("%s.%d ", parent, i)
		next := strings.Index(page[at:], want)
		if next < 0 {
			t.Fatalf("child %s is missing or out of natural order:\n%s", want, page)
		}
		at += next
	}
}

// TestUpdateChangesOnlyTheFlagsYouPass is the whole contract of update: an
// unpassed flag is not an instruction to write its zero value.
func TestUpdateChangesOnlyTheFlagsYouPass(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "create", "before", "--inbox", "-t", "bug", "-p", "0", "-d", "the body", "-A", "alice")

	mustRun(t, db, cwd, "update", id, "--title", "after")

	var view struct {
		Title    string  `json:"title"`
		Type     string  `json:"issue_type"`
		Priority int     `json:"priority"`
		Body     string  `json:"description"`
		Assignee *string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, db, cwd, "show", id, "--json")), &view); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if view.Title != "after" {
		t.Fatalf("title = %q, want after", view.Title)
	}
	if view.Type != "bug" || view.Priority != 0 || view.Body != "the body" {
		t.Fatalf("update --title rewrote a field it was not given: %#v "+
			"(priority especially: its flag has a non-zero default)", view)
	}
	if view.Assignee == nil || *view.Assignee != "alice" {
		t.Fatalf("assignee = %v, want alice left alone", view.Assignee)
	}
}

// TestClaimAssignsOnlyAnUnclaimedIssue pins claim as the safe wayfinder
// acquisition: it assigns a free ticket and refuses to overwrite another
// session's claim.
func TestClaimAssignsOnlyAnUnclaimedIssue(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "available")

	if got := mustRun(t, db, cwd, "claim", id, "alice"); got != "claimed "+id+" by alice" {
		t.Fatalf("claim output = %q, want the issue and claimant", got)
	}
	if _, _, code := run(t, db, cwd, "claim", id, "bob"); code != 5 {
		t.Fatalf("claiming an assigned issue exit = %d, want 5 (conflict)", code)
	}
	if _, _, code := run(t, db, cwd, "claim", id, ""); code != 2 {
		t.Fatalf("claiming for an empty name exit = %d, want 2 (invalid input)", code)
	}

	var view struct {
		Assignee *string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, db, cwd, "show", id, "--json")), &view); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if view.Assignee == nil || *view.Assignee != "alice" {
		t.Fatalf("assignee after refused claims = %v, want alice", view.Assignee)
	}
}

func TestClaimTreatsEmptyAssigneeAsUnclaimed(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "historically empty")
	mustRun(t, db, cwd, "update", id, "-A", "")

	if got := mustRun(t, db, cwd, "claim", id, "alice"); got != "claimed "+id+" by alice" {
		t.Fatalf("claim over an empty assignee = %q, want alice to acquire the issue", got)
	}
}

// TestReleaseClearsTheClaim pins the other half of the operation: release
// writes an unassigned value that a later session can claim.
func TestReleaseClearsTheClaim(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "claimed")
	mustRun(t, db, cwd, "claim", id, "alice")

	if got := mustRun(t, db, cwd, "release", id); got != "released "+id {
		t.Fatalf("release output = %q, want the issue", got)
	}
	var view struct {
		Assignee *string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, db, cwd, "show", id, "--json")), &view); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if got := storedAssignee(t, db, id); got.Valid {
		t.Fatalf("assignee column after release = %#v, want NULL", got)
	}
	if view.Assignee != nil {
		t.Fatalf("assignee after release = %q, want null", *view.Assignee)
	}
	if got := mustRun(t, db, cwd, "claim", id, "bob"); got != "claimed "+id+" by bob" {
		t.Fatalf("claim after release = %q, want bob to acquire the issue", got)
	}
}

// TestUpdateStatusMovesTheGlyph: --status is the in_progress path, and the
// listing glyph is how a reader sees it.
func TestUpdateStatusMovesTheGlyph(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "in flight")
	mustRun(t, db, cwd, "update", id, "--status", "in_progress")
	if row := mustRun(t, db, cwd, "list", "--inbox"); !strings.HasPrefix(row, "◐ "+id) {
		t.Fatalf("row after --status in_progress = %q, want the in_progress glyph", row)
	}
	if _, _, code := run(t, db, cwd, "update", id, "--status", "nonsense"); code != 2 {
		t.Fatalf("update --status nonsense exit = %d, want 2", code)
	}
}

// TestCloseRecordsItsReasonOnTheIssue and TestReopenClearsTheCloseReason are
// the two halves of the lifecycle a wayfinder ticket ends on: close --reason is
// where a resolution is stored, and show is where it is read back.
func TestCloseRecordsItsReasonOnTheIssue(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "a ticket")
	if got := mustRun(t, db, cwd, "close", id, "--reason", "settled: cobra stays"); got != "closed "+id {
		t.Fatalf("close printed %q", got)
	}

	var view struct {
		Status   string  `json:"status"`
		Reason   *string `json:"close_reason"`
		ClosedAt *string `json:"closed_at"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, db, cwd, "show", id, "--json")), &view); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if view.Status != "closed" || view.Reason == nil || *view.Reason != "settled: cobra stays" {
		t.Fatalf("after close: %#v", view)
	}
	if view.ClosedAt == nil {
		t.Fatal("close left closed_at null")
	}
	if page := mustRun(t, db, cwd, "show", id); !strings.Contains(page, "closed: settled: cobra stays") {
		t.Fatalf("page does not print the reason under `closed: `:\n%s", page)
	}
}

func TestReopenClearsTheCloseReason(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "a ticket")
	mustRun(t, db, cwd, "close", id, "--reason", "done")
	if got := mustRun(t, db, cwd, "reopen", id); got != "reopened "+id {
		t.Fatalf("reopen printed %q", got)
	}

	var view struct {
		Status   string  `json:"status"`
		Reason   *string `json:"close_reason"`
		ClosedAt *string `json:"closed_at"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, db, cwd, "show", id, "--json")), &view); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if view.Status != "open" {
		t.Fatalf("status after reopen = %q, want open", view.Status)
	}
	if view.Reason != nil || view.ClosedAt != nil {
		t.Fatalf("reopen left the close behind: reason=%v closed_at=%v", view.Reason, view.ClosedAt)
	}
	if row := mustRun(t, db, cwd, "list", "--inbox"); !strings.HasPrefix(row, "○ "+id) {
		t.Fatalf("reopened issue is not back in the default listing: %q", row)
	}
}
