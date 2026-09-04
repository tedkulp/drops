package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
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

// TestTheWarnSinkGoesToTheChannelWhenTheNavigatorSetsOne is the one change
// this map made outside internal/tui.
//
// Every write in the navigator's set runs core's credential scan, whose sink
// writes to STDERR — invisible under a full-screen program. Left alone the
// navigator would scan every comment you type for an AWS key or a private-key
// block and silently discard the finding (qy3de.7 §5). The sink is already a
// core.New parameter, so the fix is a redirect here and no core change at all.
func TestTheWarnSinkGoesToTheChannelWhenTheNavigatorSetsOne(t *testing.T) {
	warning := core.Warning{
		Entity: model.RecordRef{Kind: model.RecordIssue, Key: "iss01"},
		Field:  "description",
		Family: "AWS access key id",
	}

	t.Run("to stderr by default", func(t *testing.T) {
		var errOut bytes.Buffer
		app := &App{err: &errOut}
		app.warnSink()(warning)
		if !strings.Contains(errOut.String(), "AWS access key id") {
			t.Fatalf("stderr = %q, want the finding on it for every verb but tui", errOut.String())
		}
	})

	t.Run("to the channel once tui sets one", func(t *testing.T) {
		var errOut bytes.Buffer
		app := &App{err: &errOut, warnings: make(chan core.Warning, warnCapacity)}
		app.warnSink()(warning)
		if errOut.String() != "" {
			t.Fatalf("stderr = %q, want nothing: an alt screen would swallow it", errOut.String())
		}
		select {
		case got := <-app.warnings:
			if got != warning {
				t.Fatalf("channel carried %+v, want %+v", got, warning)
			}
		default:
			t.Fatal("the channel is empty; the finding went nowhere at all")
		}
	})

	t.Run("a full channel never wedges a commit", func(t *testing.T) {
		// The write has already committed by the time the sink runs, so a
		// sink that blocked would hang the program over advisory output.
		var errOut bytes.Buffer
		app := &App{err: &errOut, warnings: make(chan core.Warning)}
		done := make(chan struct{})
		go func() { app.warnSink()(warning); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the sink blocked on a channel nobody was draining")
		}
	})
}

// TestTUITakesNoAuthorFlag pins qy3de.7 §4. The flag exists on `comment add`
// so an AGENT can say who it is, and there is no agent here: an agent cannot
// drive an alt-screen program, so a comment written from the navigator comes
// from a human at a keyboard, always. A wrong name is fixed where
// resolveAuthor already reads it — `git config --global user.name`.
func TestTUITakesNoAuthorFlag(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	_, _, err := RunForTest([]string{"tui", "--author", "somebody"}, db, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --author") {
		t.Fatalf("error = %v, want tui to reject --author", err)
	}
}

// TestTUIMintsTheReplicaSidecarBeforeTakingTheScreen pins the one exception to
// "the sidecar is minted on the first local write". The navigator writes, and
// once the alt screen is up a failure to mint has nowhere to be reported —
// the same reason this verb refuses to start where no project resolves.
func TestTUIMintsTheReplicaSidecarBeforeTakingTheScreen(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "drops.db")
	cwd := t.TempDir()

	// A read verb leaves the store sidecarless, which is what makes the
	// navigator's mint an exception rather than the rule.
	if _, _, code := run(t, db, cwd, "list"); code != 0 {
		t.Fatalf("list exited %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "replica.json")); !os.IsNotExist(err) {
		t.Fatalf("a read verb minted a sidecar: %v", err)
	}

	// tui refuses here — no project resolves — and the refusal comes BEFORE
	// the mint, because requireProject runs first.
	if _, _, code := run(t, db, cwd, "tui"); code != 2 {
		t.Fatalf("tui exited %d, want 2", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "replica.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused tui minted a sidecar: %v", err)
	}
}
