package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
)

func TestResolveEditorPrefersVisualThenEditorThenVi(t *testing.T) {
	for _, want := range []struct {
		name           string
		visual, editor string
		argv           []string
	}{
		{"neither set falls back to vi", "", "", []string{"vi"}},
		{"EDITOR alone", "", "nano", []string{"nano"}},
		{"VISUAL wins over EDITOR", "hx", "nano", []string{"hx"}},
		// Split on spaces, so a value carrying flags runs as written. Both
		// of these are ordinary values for the variable.
		{"flags survive", "code -w", "", []string{"code", "-w"}},
		{"whitespace is not a setting", "   ", "emacsclient -nw", []string{"emacsclient", "-nw"}},
	} {
		t.Run(want.name, func(t *testing.T) {
			got := ResolveEditor(want.visual, want.editor)
			if strings.Join(got, "\x00") != strings.Join(want.argv, "\x00") {
				t.Fatalf("ResolveEditor(%q, %q) = %v, want %v",
					want.visual, want.editor, got, want.argv)
			}
		})
	}
}

// runEditor drives the whole round trip against a REAL editor process: the
// temp file the pane opens, the argv it assembles, the editor's own exit code,
// and the message that comes back.
//
// It stops short of tea.ExecProcess, and deliberately: a headless program
// cannot re-capture a piped input after an exec, so nothing can be driven past
// one, and what is left out is one line of bubbletea's plumbing rather than
// anything this package decides. A real process rather than a fake, per
// AGENTS.md — what an editor's file and exit code do to the store is the whole
// question, so faking the editor would fake the answer.
func runEditor(t *testing.T, pane *Model, kind editKind, id model.ID) string {
	t.Helper()
	command, path, err := pane.editorCommand()
	if err != nil {
		t.Fatalf("editorCommand: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the editor was handed no file: %v", err)
	}
	pane.finishEdit(editorDoneMsg{kind: kind, id: id, path: path, err: command.Run()})
	return path
}

func TestTheRoundTripClosesAnIssueThroughARealEditorProcess(t *testing.T) {
	requireShell(t)
	fixture := newFixture(t)
	issue := fixture.issue("An issue closed through a real editor", 1)
	fixture.editor = editorWrites("closed from the navigator\n")

	pane := fixture.model(80, 24)
	// `x` then `enter` on `close` is what asks for the editor, and it comes
	// back as a command rather than a write, because the program has to pause
	// for it.
	openActionsOn(t, pane)
	if cmd := chooseRow(t, pane, "close"); cmd == nil {
		t.Fatal("enter on `close` returned no command; nothing would run the editor")
	}
	path := runEditor(t, pane, editClose, issue.ID)

	got := fixture.reread(issue.ID)
	if got.Status != model.StatusClosed {
		t.Fatalf("status = %s, want closed", got.Status)
	}
	if got.CloseReason == nil || *got.CloseReason != "closed from the navigator" {
		t.Fatalf("close reason = %v, want the editor's own text", got.CloseReason)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the editor's file survived the write: %v", err)
	}
}

func TestARealEditorExitingNonZeroWritesNothing(t *testing.T) {
	// `:cq`, as a real process: the file is full of text and the exit code is
	// what discards it.
	requireShell(t)
	fixture := newFixture(t)
	issue := fixture.issue("An issue whose close is cancelled with :cq", 1)
	fixture.editor = shEditor(`printf %s 'a reason typed and then abandoned' > "$1"; exit 1`)

	pane := fixture.model(80, 24)
	runEditor(t, pane, editClose, issue.ID)

	if got := fixture.reread(issue.ID); got.Status != model.StatusOpen {
		t.Fatalf("status = %s, want it still open", got.Status)
	}
	if !strings.Contains(pane.message, "exit status 1") {
		t.Fatalf("message = %q, want the editor's failure on it", pane.message)
	}
}

func TestARealEditorLeavingTheFileEmptyWritesNothing(t *testing.T) {
	// The default fixture editor is `:` — it runs, changes nothing, and exits
	// 0, which is quitting an editor without typing.
	requireShell(t)
	fixture := newFixture(t)
	issue := fixture.issue("An issue whose comment is abandoned", 1)

	pane := fixture.model(80, 24)
	runEditor(t, pane, editComment, issue.ID)

	if got := fixture.comments(issue.ID); len(got) != 0 {
		t.Fatalf("thread holds %d comments, want none", len(got))
	}
	if pane.message != "" {
		t.Fatalf("message = %q, want silence", pane.message)
	}
}

func TestTheEditorArgvCarriesItsFlagsAndTheFileLast(t *testing.T) {
	// $EDITOR values carrying flags are ordinary — `code -w`, `emacsclient
	// -nw` — and the file has to be the last argument for any of them.
	requireShell(t)
	fixture := newFixture(t)
	fixture.issue("An issue", 1)
	seen := filepath.Join(t.TempDir(), "argv")
	fixture.editor = shEditor(`printf '%s\n' "$@" > ` + shellQuote(seen))
	fixture.editor = append(fixture.editor, "--flag", "value")

	pane := fixture.model(80, 24)
	command, path, err := pane.editorCommand()
	if err != nil {
		t.Fatalf("editorCommand: %v", err)
	}
	if err := command.Run(); err != nil {
		t.Fatalf("run editor: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	raw, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	if got := strings.Fields(string(raw)); len(got) != 3 ||
		got[0] != "--flag" || got[1] != "value" || got[2] != path {
		t.Fatalf("editor saw %v, want [--flag value %s]", got, path)
	}
}

func requireShell(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh to stand in for $EDITOR: %v", err)
	}
}
