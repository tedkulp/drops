package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/model"
)

// editKind is which of the two typing verbs asked for the editor. It rides on
// the message rather than on the model because the program is PAUSED while the
// editor runs: what comes back has to say what it was for.
type editKind int

const (
	editClose editKind = iota
	editComment
)

// editorDoneMsg is the editor's exit, delivered by tea.ExecProcess's callback.
type editorDoneMsg struct {
	kind editKind
	id   model.ID
	path string
	err  error
}

// DefaultEditor is what cli resolves $VISUAL and $EDITOR down to when neither
// is set. It is exported so the resolution lives in one place and cli's own
// test can name the fallback rather than repeat the string.
const DefaultEditor = "vi"

// ResolveEditor is $VISUAL, then $EDITOR, then vi, split on spaces so a value
// carrying flags — `code -w`, `emacsclient -nw` — runs as written.
//
// It lives here beside the code that spends it, and is CALLED by cli: the
// environment is a discovered fact, so the answer is a parameter to New rather
// than something looked up at the point of use (AGENTS.md's seam rule). That
// is also what lets the whole editor round trip be tested against a real
// process without touching the process's own environment.
func ResolveEditor(visual, editor string) []string {
	for _, candidate := range []string{visual, editor} {
		if fields := strings.Fields(candidate); len(fields) > 0 {
			return fields
		}
	}
	return []string{DefaultEditor}
}

// edit opens the editor on an empty temp .md and returns the command that runs
// it. tea.ExecProcess pauses and resumes the Program itself, so the alt screen
// needs no handling on either side.
//
// The file arrives EMPTY: no seeded header and no `#` stripping, because six
// close reasons in the corpus start a line with `#` and a stripper would eat
// them. The extension is .md so the editor's own syntax highlighting is right.
func (m *Model) edit(kind editKind, id model.ID) tea.Cmd {
	command, path, err := m.editorCommand()
	if err != nil {
		m.message = "error: " + err.Error()
		return nil
	}
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return editorDoneMsg{kind: kind, id: id, path: path, err: err}
	})
}

// editorCommand opens the empty temp file and builds the process that edits
// it. It is separate from edit for one reason: everything uncertain about the
// round trip is HERE and in finishEdit — the argv, the file, the exit code —
// while edit is the one line of bubbletea plumbing between them. Split this
// way the whole thing is exercised against a real editor process without a
// tea.Program, which matters because a headless program cannot re-capture a
// piped input after an exec and so cannot be driven past one.
func (m *Model) editorCommand() (*exec.Cmd, string, error) {
	file, err := os.CreateTemp("", "drops-*.md")
	if err != nil {
		return nil, "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return nil, path, err
	}
	argv := append(append([]string{}, m.editor...), path)
	return exec.Command(argv[0], argv[1:]...), path, nil
}

// finishEdit is what the editor's exit does, and it is where two of qy3de.7's
// three refusals live.
//
// A NON-ZERO EXIT writes nothing and says why, which is what makes `:cq` a
// deliberate cancel rather than a way to lose what you typed silently.
//
// An EMPTY or whitespace-only file writes nothing and says nothing: you opened
// the editor, changed your mind, and quit. For a comment that is free, since
// AddComment rejects an empty body as ErrInvalid anyway; for a close it is a
// real divergence from the CLI, where `--reason` is optional and `drops close
// <id>` legally stores NULL. It has happened twice in 838 closings, and
// AGENTS.md's "What a ticket records" is why the navigator does not offer the
// third case at all.
func (m *Model) finishEdit(done editorDoneMsg) {
	defer func() { _ = os.Remove(done.path) }()

	if done.err != nil {
		m.message = "editor: " + done.err.Error()
		return
	}
	raw, err := os.ReadFile(done.path)
	if err != nil {
		m.message = "error: " + err.Error()
		return
	}
	// Trimmed rather than stored raw: every editor writes a trailing
	// newline, and it is the same trim that decides the file was empty.
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return
	}
	switch done.kind {
	case editClose:
		m.write(func() error {
			_, err := m.source.core.SetIssueStatus(m.ctx, done.id, model.StatusClosed, text)
			return err
		})
	case editComment:
		m.write(func() error {
			_, err := m.source.core.AddComment(m.ctx, done.id, m.author, text)
			return err
		})
	}
}
