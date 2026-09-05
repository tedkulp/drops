package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/model"
)

// copied presses `y` and runs what it handed back, returning the message that
// produced. Run rather than inspected because a tea.Cmd is a func: the Msg is
// the only thing about it that can be compared.
func copied(t *testing.T, pane *Model) tea.Msg {
	t.Helper()
	cmd := press(t, pane, "y")
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestYCopiesTheRightPanesIDNotTheCursorRows(t *testing.T) {
	// The two differ exactly while the follow stack is non-empty, and a test
	// that only asserted "some clipboard command came back" would pass with
	// the cursor's id in place of the pane's — which is the whole decision.
	fixture := newFixture(t)
	parent := fixture.issue("The issue the cursor is on", 1)
	child := fixture.child(parent.ID, "The issue that is followed to", 2)

	pane := fixture.model(80, 24)
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.detailID != child.ID || pane.cursor.id != parent.ID {
		t.Fatalf("after following, detail = %s and cursor = %s; want %s and %s",
			pane.detailID, pane.cursor.id, child.ID, parent.ID)
	}

	// setClipboardMsg is unexported and comparable, so the assertion is
	// interface equality against the message the real constructor makes.
	// Nothing here fakes the clipboard: OSC 52 is what bubbletea writes when
	// it receives this, and the terminal never answers it either way.
	got := copied(t, pane)
	if want := tea.SetClipboard(string(child.ID))(); got != want {
		t.Fatalf("y sent %#v, want the right pane's issue %s (%#v)", got, child.ID, want)
	}
	if got == tea.SetClipboard(string(parent.ID))() {
		t.Fatalf("y sent the cursor row's id %s, not the followed issue %s", parent.ID, child.ID)
	}
}

func TestYSaysOnTheFooterWhatItSent(t *testing.T) {
	// What was SENT, not what was pasted: OSC 52 is unacknowledged, so a
	// notice claiming the clipboard now holds this would be a claim the
	// program cannot make.
	fixture := newFixture(t)
	only := fixture.issue("The issue on the right", 1)

	pane := fixture.model(80, 24)
	copied(t, pane)
	if want := "sent " + string(only.ID) + " to the clipboard"; pane.notice != want {
		t.Fatalf("notice = %q, want %q", pane.notice, want)
	}
	// The footer's left side and not a pane row: m.message costs one, and
	// the panes must not relayout for a keystroke that wrote nothing.
	if pane.message != "" {
		t.Fatalf("message = %q, want y to leave the message line alone", pane.message)
	}
	if footer := pane.footer(pane.geo()); !strings.Contains(footer, string(only.ID)) {
		t.Fatalf("footer = %q, want it to name the id that was sent", footer)
	}

	// A notice lives until the next keypress and no longer.
	press(t, pane, "j")
	if pane.notice != "" {
		t.Fatalf("notice = %q, want it gone after the next key", pane.notice)
	}
}

func TestYOnAnEmptyRowSetSendsNothing(t *testing.T) {
	// There is no issue on the right to name, and reporting an empty id as
	// sent is the output the refusal exists to prevent. It is the same
	// refusal `x` makes there.
	fixture := newFixture(t)
	pane := fixture.model(80, 24)
	if len(pane.rows) != 0 || pane.detailID != model.ID("") {
		t.Fatalf("fixture has %d rows showing %q, want an empty pane", len(pane.rows), pane.detailID)
	}

	if got := copied(t, pane); got != nil {
		t.Fatalf("y on an empty pane sent %#v, want nothing", got)
	}
	if pane.notice != "" {
		t.Fatalf("notice = %q, want nothing reported as sent", pane.notice)
	}
}
