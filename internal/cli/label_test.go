package cli

import (
	"strings"
	"testing"
)

// TestLabelAddAndRmChangeTheIssuesLabels covers both mutating label verbs by
// their store effect. Both are variadic, and both go through the same live/dead
// filter, so a removed label must vanish from every read path rather than
// linger as a tombstoned row somebody forgot to filter.
func TestLabelAddAndRmChangeTheIssuesLabels(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "create", "labelled", "--inbox", "-l", "first")

	if got := mustRun(t, db, cwd, "label", "add", id, "second", "third"); got != "labelled "+id+": [second third]" {
		t.Fatalf("label add printed %q", got)
	}
	if got := decodeMany[string](t, mustRun(t, db, cwd, "label", "list", id, "--json")); strings.Join(got, ",") != "first,second,third" {
		t.Fatalf("labels after add = %#v", got)
	}

	if got := mustRun(t, db, cwd, "label", "rm", id, "first", "third"); got != "removed from "+id+": [first third]" {
		t.Fatalf("label rm printed %q", got)
	}
	if got := decodeMany[string](t, mustRun(t, db, cwd, "label", "list", id, "--json")); strings.Join(got, ",") != "second" {
		t.Fatalf("labels after rm = %#v, want only second", got)
	}

	// Every other read path agrees: the page's identity strip and the
	// server-side list filter.
	if page := mustRun(t, db, cwd, "show", id); strings.Contains(page, "first") || !strings.Contains(page, "second") {
		t.Fatalf("the page's label strip disagrees with label list:\n%s", page)
	}
	if out := mustRun(t, db, cwd, "list", "--inbox", "-l", "first"); strings.Contains(out, id) {
		t.Fatalf("list -l still matched a removed label:\n%s", out)
	}
	if out := mustRun(t, db, cwd, "list", "--inbox", "-l", "second"); !strings.Contains(out, id) {
		t.Fatalf("list -l missed a live label:\n%s", out)
	}
	// show --json omits labels entirely when there are none — the one key
	// the doc warns is not safe to index.
	stripped := mustRun(t, db, cwd, "create", "bare", "--inbox")
	if view := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", stripped, "--json")); view["labels"] != nil {
		t.Fatalf("labels = %v on an issue carrying none, want the key absent", view["labels"])
	}
}

// TestLabelListNeedsBothArguments: add and rm each take an id and at least one
// label, so a one-argument call is a misuse rather than a no-op.
func TestLabelAddAndRmNeedALabel(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "an issue")
	for _, verb := range []string{"add", "rm"} {
		if _, _, code := run(t, db, cwd, "label", verb, id); code != 2 {
			t.Errorf("label %s with no label exit = %d, want 2", verb, code)
		}
	}
	// list is the one that takes at most one, because with none it counts.
	if _, _, code := run(t, db, cwd, "label", "list", id, "extra"); code != 2 {
		t.Errorf("label list with two positionals exit = %d, want 2", code)
	}
}
