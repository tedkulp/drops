package cli

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCommentAddPrintsTheCommentID: the id is the verb's whole output, because
// it is the one argument `comment rm` takes, and it carries its own issue —
// `<issue-id>:<12 hex>`.
func TestCommentAddPrintsTheCommentID(t *testing.T) {
	db, cwd := newStore(t)
	issue := mustRun(t, db, cwd, "q", "--inbox", "an issue")

	out, _, code := run(t, db, cwd, "comment", "add", issue, "a remark", "--author", "agent")
	if code != 0 {
		t.Fatalf("comment add exit = %d", code)
	}
	id := strings.TrimSpace(out)
	if out != id+"\n" {
		t.Fatalf("comment add printed %q, want exactly the id", out)
	}
	prefix, hex, ok := strings.Cut(id, ":")
	if !ok || prefix != issue || len(hex) != 12 {
		t.Fatalf("comment id = %q, want %s:<12 hex>", id, issue)
	}

	thread := decodeMany[map[string]any](t, mustRun(t, db, cwd, "comment", "list", issue, "--json"))
	if len(thread) != 1 || thread[0]["id"] != id || thread[0]["body"] != "a remark" || thread[0]["author"] != "agent" {
		t.Fatalf("stored comment = %#v", thread)
	}
	// A closed issue still accepts comments: a resolution is not the end of
	// the conversation about it.
	mustRun(t, db, cwd, "close", issue, "--reason", "done")
	if _, _, code := run(t, db, cwd, "comment", "add", issue, "an afterthought", "--author", "agent"); code != 0 {
		t.Fatalf("a closed issue refused a comment: exit %d", code)
	}
}

// TestCommentAuthorDefaultsToGitThenTheOSUser is why the doc says PASS
// --author: the default is right for a human and wrong for an agent, so an
// agent that omits it files a false record under the repo owner's name.
func TestCommentAuthorDefaultsToGitThenTheOSUser(t *testing.T) {
	db, cwd := newStore(t)
	issue := mustRun(t, db, cwd, "q", "--inbox", "an issue")

	mustRun(t, db, cwd, "comment", "add", issue, "explicit", "--author", "the-agent")
	mustRun(t, db, cwd, "comment", "add", issue, "implicit")

	thread := decodeMany[map[string]any](t, mustRun(t, db, cwd, "comment", "list", issue, "--json"))
	if len(thread) != 2 {
		t.Fatalf("thread = %#v, want two comments", thread)
	}
	if thread[0]["author"] != "the-agent" {
		t.Errorf("--author was not used verbatim: %v", thread[0]["author"])
	}
	fallback, _ := thread[1]["author"].(string)
	if fallback == "the-agent" {
		t.Fatalf("the default author leaked the previous --author: %q", fallback)
	}
	if want := gitGlobalUserName(); want != "" && fallback != want {
		t.Errorf("default author = %q, want the global git user.name %q", fallback, want)
	}
	if fallback == "" {
		t.Error("default author is empty; it should fall back to git user.name, then the OS username")
	}
}

func gitGlobalUserName() string {
	out, err := exec.Command("git", "config", "--global", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestCommentListRendersTheThreadOldestFirst pins the thread block: the count
// leads, each header is date · author · id, and the body is indented two
// columns. It is the SAME block `show` prints, which is what the doc promises
// and what keeps a listing and a page from disagreeing.
func TestCommentListRendersTheThreadOldestFirst(t *testing.T) {
	db, cwd := newStore(t)
	issue := mustRun(t, db, cwd, "q", "--inbox", "an issue")
	first := mustRun(t, db, cwd, "comment", "add", issue, "the first remark", "--author", "agent")
	second := mustRun(t, db, cwd, "comment", "add", issue, "the second remark", "--author", "agent")

	out, _, code := run(t, db, cwd, "comment", "list", issue)
	if code != 0 {
		t.Fatalf("comment list exit = %d", code)
	}
	if !strings.HasPrefix(out, "Comments  2\n") {
		t.Fatalf("thread does not lead with its count: %q", out)
	}
	if strings.Index(out, first) > strings.Index(out, second) {
		t.Fatalf("thread is not oldest first:\n%s", out)
	}
	if !strings.Contains(out, " · agent · "+first+"\n") {
		t.Fatalf("comment header is not date · author · id:\n%s", out)
	}
	if !strings.Contains(out, "\n  the first remark\n") {
		t.Fatalf("comment body is not indented two columns:\n%s", out)
	}

	// The page prints the identical block, so the two can never drift.
	page, _, _ := run(t, db, cwd, "show", issue)
	at := strings.Index(page, "Comments  2\n")
	if at < 0 {
		t.Fatalf("show printed no comment thread at all:\n%s", page)
	}
	if got := page[at:]; got != out {
		t.Fatalf("show's thread and comment list's thread differ:\n%q\nvs\n%q", got, out)
	}

	// An empty thread prints no heading, and --json is [] rather than null.
	bare := mustRun(t, db, cwd, "q", "--inbox", "no discussion")
	if got, _, _ := run(t, db, cwd, "comment", "list", bare); got != "" {
		t.Fatalf("an empty thread printed %q, want nothing", got)
	}
	if got := mustRun(t, db, cwd, "comment", "list", bare, "--json"); got != "[]" {
		t.Fatalf("empty thread --json = %q, want []", got)
	}
}

// TestCommentRmDropsOneCommentFromTheThread: a hard delete, for undoing a
// mistyped comment. There is deliberately no comment edit, so this is the only
// way a comment leaves a thread.
func TestCommentRmDropsOneCommentFromTheThread(t *testing.T) {
	db, cwd := newStore(t)
	issue := mustRun(t, db, cwd, "q", "--inbox", "an issue")
	keep := mustRun(t, db, cwd, "comment", "add", issue, "worth keeping", "--author", "agent")
	drop := mustRun(t, db, cwd, "comment", "add", issue, "a mistype", "--author", "agent")

	if got := mustRun(t, db, cwd, "comment", "rm", drop); got != "removed comment "+drop {
		t.Fatalf("comment rm printed %q", got)
	}
	thread := decodeMany[map[string]any](t, mustRun(t, db, cwd, "comment", "list", issue, "--json"))
	if len(thread) != 1 || thread[0]["id"] != keep {
		t.Fatalf("thread after rm = %#v, want only %s", thread, keep)
	}
	// It is gone rather than tombstoned, so the search index loses it too.
	if out := mustRun(t, db, cwd, "search", "mistype", "--inbox"); strings.Contains(out, issue) {
		t.Fatalf("a removed comment is still findable:\n%s", out)
	}
	// The id carries its issue, so no issue argument is needed — and an id
	// naming no comment is a not-found refusal.
	if _, _, code := run(t, db, cwd, "comment", "rm", issue+":000000000000"); code != 4 {
		t.Fatalf("comment rm of an unknown id exit = %d, want 4", code)
	}
}
