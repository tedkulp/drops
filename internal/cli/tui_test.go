package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestTUIRefusesToStartWhereNoProjectResolves is `tui`'s boundary test: the one
// place this verb is deliberately asymmetric with the read verbs.
//
// `list`, `ready` and `config show` print "no project resolves here" to stderr,
// exit 0 and write nothing to stdout. `tui` cannot: a stderr advisory is
// invisible under an alt screen, so exiting 0 would put an empty pane on the
// terminal with no explanation of why.
func TestTUIRefusesToStartWhereNoProjectResolves(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	// A bare temp directory: no repository, no binding, no locator, and an
	// empty store, so nothing can resolve and the navigator is never started.
	out, errOut, code := run(t, db, t.TempDir(), "tui")

	if code != 2 {
		t.Fatalf("tui with no project resolved exited %d, want 2 (invalid input); stderr %q", code, errOut)
	}
	if out != "" {
		t.Fatalf("tui wrote %q to stdout; a refusal writes nothing there", out)
	}
	// The advisory the read verbs print is deliberately absent: the refusal
	// IS the error, so an operator sees one message, not two.
	if strings.Contains(errOut, "no project resolves here") {
		t.Fatalf("stderr = %q, want the refusal carried by the error alone", errOut)
	}
}

// TestTUIRefusalNamesTheWayOut checks the message itself, because an operator
// who sees only an exit code learns nothing. RunForTest does not print the
// error — Execute does — so the message is read off the error value.
func TestTUIRefusalNamesTheWayOut(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	_, _, err := RunForTest([]string{"tui"}, db, t.TempDir())
	if err == nil {
		t.Fatal("tui with no project resolved returned no error")
	}
	if !strings.Contains(err.Error(), "no project resolves here; pass -P <slug> to scope explicitly") {
		t.Fatalf("error = %q, want it to name -P as the way out", err)
	}
}

func TestTUITakesNoPositionalArguments(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	if _, _, code := run(t, db, t.TempDir(), "tui", "k3f9x"); code != 2 {
		t.Fatalf("tui with a positional exited %d, want 2", code)
	}
}
