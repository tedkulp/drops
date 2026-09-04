package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The poll's tests all drive the model directly rather than through a
// tea.Program. That is not a shortcut around a real terminal — qy3de.11 and
// qy3de.13 both ran their frames on one — it is what the timer forces: a test
// that waited out a real 2s tick would put the interval in the suite's runtime
// and still assert nothing the tickMsg path does not already say.

// tickPane delivers one tickMsg the way bubbletea would, and returns the
// command that came back so the re-arm can be asserted.
func tickPane(t *testing.T, pane *Model) tea.Cmd {
	t.Helper()
	_, cmd := pane.Update(tickMsg{})
	return cmd
}

func TestTickNoticesWithoutReloading(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("The one on screen", 2)
	pane := fixture.model(80, 24)

	before := ids(pane.rows)
	fixture.issue("Written by somebody else", 2)

	tickPane(t, pane)

	// The whole of §Q1: the tick's job is NOTICING. A pane that reloaded
	// here would reorder itself under a reader mid-thought, which is the
	// thing this map exists to avoid.
	if got := ids(pane.rows); len(got) != len(before) {
		t.Fatalf("row set = %v, want the tick to leave it at %v", got, before)
	}
	if !pane.stale() {
		t.Fatalf("pane is not stale after another writer wrote")
	}
}

func TestTickReArmsThePoll(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("An issue", 2)
	pane := fixture.model(80, 24)

	// A tick re-arms on the one path that handles it. Nothing else does, so
	// a tick that came back with no command would stop the poll for the rest
	// of the session with nothing on screen to say so.
	if cmd := tickPane(t, pane); cmd == nil {
		t.Fatalf("a tick returned no command, so the poll is dead")
	}
	// And the command it returns is another tick, not some other timer.
	if cmd := tickPane(t, pane); cmd == nil {
		t.Fatalf("the second tick returned no command")
	} else if _, ok := cmd().(tickMsg); !ok {
		t.Fatalf("a tick re-armed with %T, want another tickMsg", cmd())
	}
}

func TestInitArmsTheFirstTick(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("An issue", 2)
	pane := fixture.model(80, 24)

	cmd := pane.Init()
	if cmd == nil {
		t.Fatalf("Init armed no tick, so the poll never starts")
	}
	if _, ok := cmd().(tickMsg); !ok {
		t.Fatalf("Init armed %T, want a tickMsg", cmd())
	}
}

func TestStaleMarkerIsAFooterPartAndSurvivesAKeypress(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("The one on screen", 2)
	pane := fixture.model(80, 24)

	geo := pane.geo()
	rowsBefore := geo.rows
	if strings.Contains(pane.footer(geo), "stale") {
		t.Fatalf("footer = %q, want no marker on a fresh pane", pane.footer(geo))
	}

	fixture.issue("Written by somebody else", 2)
	tickPane(t, pane)

	footer := pane.footer(pane.geo())
	if !strings.Contains(footer, "stale") {
		t.Fatalf("footer = %q, want the stale marker", footer)
	}
	// Beside the mode words, not instead of them: m.notice replaces this
	// whole left side, which would hide qy3de.4's disclosure line.
	if !strings.Contains(footer, "drops") {
		t.Fatalf("footer = %q, want the scope still disclosed beside the marker", footer)
	}

	// And it costs no pane row: m.message would, because messageLines feeds
	// geo, so a marker there shrinks both panes while a picker is open.
	if got := pane.geo().rows; got != rowsBefore {
		t.Fatalf("pane rows = %d after the marker, want %d", got, rowsBefore)
	}
	if pane.messageLines() != 0 {
		t.Fatalf("the marker took the message row, which resizes the panes")
	}

	// It survives a keypress, unlike both transient slots: press `j` and it
	// is still stale, because the store still is.
	press(t, pane, "j")
	if !strings.Contains(pane.footer(pane.geo()), "stale") {
		t.Fatalf("footer = %q, want the marker to survive a keypress", pane.footer(pane.geo()))
	}
}

func TestRefreshClearsTheStaleMarker(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("The one on screen", 2)
	pane := fixture.model(80, 24)

	fixture.issue("Written by somebody else", 2)
	tickPane(t, pane)
	if !pane.stale() {
		t.Fatalf("pane is not stale after another writer wrote")
	}

	press(t, pane, "r")

	// Cleared by the refresh that read past it, and by nothing else.
	if pane.stale() {
		t.Fatalf("pane is still stale after `r`")
	}
	if strings.Contains(pane.footer(pane.geo()), "stale") {
		t.Fatalf("footer = %q, want no marker after `r`", pane.footer(pane.geo()))
	}
	if len(pane.rows) != 2 {
		t.Fatalf("rows = %d after `r`, want the new issue read in", len(pane.rows))
	}
}

func TestRefreshRereadsWhenNothingIsStale(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("The one on screen", 2)
	pane := fixture.model(80, 24)

	// §Q6: `r` is UNCONDITIONAL. Somebody writes and the tick has not yet
	// fired, so nothing on the pane knows — and `r` still reads it in. An
	// `r` that declined when it believed itself fresh would be the key you
	// press twice, and would trust a token over the store.
	fixture.issue("Written between two ticks", 2)
	if pane.stale() {
		t.Fatalf("pane should not know about the write yet")
	}

	press(t, pane, "r")

	if len(pane.rows) != 2 {
		t.Fatalf("rows = %d, want `r` to have re-read regardless of the marker", len(pane.rows))
	}
}

func TestRefreshKeepsTheFollowStack(t *testing.T) {
	fixture := newFixture(t)
	parent := fixture.issue("The parent", 2)
	fixture.child(parent.ID, "The child", 2)
	pane := fixture.model(80, 24)

	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	followed := pane.detailID
	if len(pane.stack) != 1 || followed == pane.cursor.id {
		t.Fatalf("follow did not retarget the right pane: stack=%v detail=%s cursor=%s",
			pane.stack, followed, pane.cursor.id)
	}

	press(t, pane, "r")

	// `r` is refresh and not reload: re-reading the store is not a
	// navigation, and reload clears the stack because moving the cursor is.
	if len(pane.stack) != 1 {
		t.Fatalf("stack = %v after `r`, want the trail kept", pane.stack)
	}
	if pane.detailID != followed {
		t.Fatalf("detail = %s after `r`, want the followed target %s", pane.detailID, followed)
	}
}

func TestRedrawKeepsTheReadersPosition(t *testing.T) {
	fixture := newFixture(t)
	long := strings.Repeat("A line of the body that is worth scrolling past.\n", 60)
	issue := fixture.issueWith("A long page", long, 2)
	pane := fixture.model(80, 24)

	for range 4 {
		press(t, pane, "J")
	}
	scrolled := pane.detail.YOffset()
	if scrolled == 0 {
		t.Fatalf("the page did not scroll, so this test cannot fail for its clause")
	}

	// The page being read is the one that CHANGES: an unrelated write takes
	// the identical-text path instead, where the offsets are never touched
	// and this clause is not exercised at all.
	if _, err := fixture.core.AddComment(t.Context(), issue.ID, "somebody", "written from another terminal"); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	press(t, pane, "r")

	if !strings.Contains(pane.pageText, "written from another terminal") {
		t.Fatalf("the page did not change, so this test cannot fail for its clause")
	}
	if got := pane.detail.YOffset(); got != scrolled {
		t.Fatalf("y offset = %d after a refresh, want %d — the reader was thrown back up the page", got, scrolled)
	}
	if pane.detailID != issue.ID {
		t.Fatalf("detail = %s, want %s", pane.detailID, issue.ID)
	}
}

func TestRetargetingStartsAtTheTop(t *testing.T) {
	fixture := newFixture(t)
	long := strings.Repeat("A line of the body that is worth scrolling past.\n", 60)
	fixture.issueWith("A long page", long, 1)
	// The page moved TO is long as well, or the viewport clamps a carried
	// offset back to zero on its own and this clause cannot fail either way.
	fixture.issueWith("Another long page", long, 2)
	pane := fixture.model(80, 24)

	for range 4 {
		press(t, pane, "J")
	}
	if pane.detail.YOffset() == 0 {
		t.Fatalf("the page did not scroll, so this test cannot fail for its clause")
	}

	// The other half of the same branch: opening a DIFFERENT issue starts at
	// the top, because you asked for a different issue. Only a redraw holds.
	press(t, pane, "j")

	if got := pane.detail.YOffset(); got != 0 {
		t.Fatalf("y offset = %d on a newly opened page, want 0", got)
	}
}

func TestRedrawFollowsACommentOnThePageBeingRead(t *testing.T) {
	fixture := newFixture(t)
	issue := fixture.issue("The one on screen", 2)
	pane := fixture.model(80, 24)

	if strings.Contains(pane.pageText, "a comment from another terminal") {
		t.Fatalf("the comment is on the page before it was written")
	}

	// A comment is its OWN record with its own revision, so Issue.Revision
	// is unmoved by it. A redraw that tested the revision would decline to
	// re-render the one write most likely to change the page you are reading.
	if _, err := fixture.core.AddComment(t.Context(), issue.ID, "somebody", "a comment from another terminal"); err != nil {
		t.Fatalf("add comment: %v", err)
	}

	press(t, pane, "r")

	if !strings.Contains(pane.pageText, "a comment from another terminal") {
		t.Fatalf("page did not pick up the new comment:\n%s", pane.pageText)
	}
}

func TestModalAndPromptCannotReachARefresh(t *testing.T) {
	fixture := newFixture(t)
	parent := fixture.issue("The parent", 2)
	fixture.child(parent.ID, "The child", 2)
	pane := fixture.model(80, 24)

	// §Q7: the suppression this ticket considered building is satisfied by
	// construction, and this is the assertion that says so. A modal routes
	// every key through modalKey, whose switch has no `r`.
	press(t, pane, "f")
	if pane.modal == nil {
		t.Fatalf("the picker did not open")
	}
	fixture.issue("Written by somebody else", 2)
	before := len(pane.rows)
	press(t, pane, "r")
	if pane.modal == nil {
		t.Fatalf("`r` closed the picker")
	}
	if len(pane.rows) != before {
		t.Fatalf("rows = %d, want `r` inside a modal to reach no refresh", len(pane.rows))
	}
	pressNamed(t, pane, tea.KeyEsc)

	// And the `/` prompt swallows it into the filter, which is why a filter
	// may contain an `r` without re-reading the store.
	press(t, pane, "/")
	press(t, pane, "r")
	if pane.filter != "r" {
		t.Fatalf("filter = %q, want `r` to have been typed into it", pane.filter)
	}
	if len(pane.rows) == before {
		t.Fatalf("the filter did not narrow, so this test cannot fail for its clause")
	}
}
