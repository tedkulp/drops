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
	// It names the key that clears it, so the marker and the keymap cannot
	// drift apart: g7b23 moved refresh off `r` and this is what would have
	// left the footer telling a reader to press the narrowing key.
	if !strings.Contains(footer, "stale · R") {
		t.Fatalf("footer = %q, want the marker to name the refresh key", footer)
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

	press(t, pane, "R")

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

	press(t, pane, "R")

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

	press(t, pane, "R")

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
	long := strings.Repeat("A line of the body that is worth scrolling past.\n", 60)

	// The refresh a write leaves behind, which is the motion the clause was
	// written for. The page being read is the one that CHANGES: an unrelated
	// write takes the identical-text path instead, where the offsets are
	// never touched and this clause is not exercised at all.
	t.Run("a refresh", func(t *testing.T) {
		fixture := newFixture(t)
		issue := fixture.issueWith("A long page", long, 2)
		pane := fixture.model(80, 24)

		scrolled := scrollDetail(t, pane)
		if _, err := fixture.core.AddComment(t.Context(), issue.ID, "somebody", "written from another terminal"); err != nil {
			t.Fatalf("add comment: %v", err)
		}
		press(t, pane, "R")

		if !strings.Contains(pane.pageText, "written from another terminal") {
			t.Fatalf("the page did not change, so this test cannot fail for its clause")
		}
		if got := pane.detail.YOffset(); got != scrolled {
			t.Fatalf("y offset = %d after a refresh, want %d — the reader was thrown back up the page", got, scrolled)
		}
		if pane.detailID != issue.ID {
			t.Fatalf("detail = %s, want %s", pane.detailID, issue.ID)
		}
	})

	// And every other motion that leaves the same page on screen at the same
	// width. These reach the rule through applyFilter and reload rather than
	// refresh, and each of them reset the reader to line 0 while the pane
	// decided this on the caller's intent instead (tc2x5).
	for _, motion := range []struct {
		name string
		do   func(t *testing.T, pane *Model)
	}{
		{"a keystroke into the filter", func(t *testing.T, pane *Model) {
			press(t, pane, "/")
			press(t, pane, "l")
		}},
		{"C", func(t *testing.T, pane *Model) { press(t, pane, "C") }},
		{"a height-only resize", func(t *testing.T, pane *Model) {
			pane.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
		}},
	} {
		t.Run(motion.name, func(t *testing.T) {
			fixture := newFixture(t)
			// One row, so a motion that narrows the row set cannot move
			// the cursor onto another issue and pass for the wrong reason.
			issue := fixture.issueWith("A long page", long, 2)
			pane := fixture.model(80, 24)

			scrolled := scrollDetail(t, pane)
			motion.do(t, pane)

			if pane.detailID != issue.ID {
				t.Fatalf("detail = %s, want the page still under the reader, %s", pane.detailID, issue.ID)
			}
			if got := pane.detail.YOffset(); got != scrolled {
				t.Fatalf("y offset = %d, want %d — the reader was thrown back up a page they never left", got, scrolled)
			}
		})
	}
}

// scrollDetail moves the right pane off line 0 and returns where it landed,
// failing if it could not: an offset assertion made on an unscrolled page is
// green whatever the production branch does.
func scrollDetail(t *testing.T, pane *Model) int {
	t.Helper()
	for range 4 {
		press(t, pane, "J")
	}
	scrolled := pane.detail.YOffset()
	if scrolled == 0 {
		t.Fatalf("the page did not scroll, so this test cannot fail for its clause")
	}
	return scrolled
}

func TestRetargetingStartsAtTheTop(t *testing.T) {
	// The page moved TO is long as well, or the viewport clamps a carried
	// offset back to zero on its own and neither half can fail either way.
	long := strings.Repeat("A line of the body that is worth scrolling past.\n", 60)

	// The reader's own motion: opening a DIFFERENT issue starts at the top,
	// because you asked for a different issue. Only a redraw holds.
	t.Run("the cursor moves", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.issueWith("A long page", long, 1)
		fixture.issueWith("Another long page", long, 2)
		pane := fixture.model(80, 24)

		scrollDetail(t, pane)
		press(t, pane, "j")

		if got := pane.detail.YOffset(); got != 0 {
			t.Fatalf("y offset = %d on a newly opened page, want 0", got)
		}
	})

	// The writer's (tc2x5). A refresh redraws with the reader's position
	// kept, but the same refresh can move the cursor onto a different issue:
	// closing the cursor's own row with `C` off is the case refresh's doc
	// comment describes. That page is one the reader has never scrolled, so
	// intent is not enough — the retarget is decided on IDENTITY.
	t.Run("a refresh lands on another issue", func(t *testing.T) {
		fixture := newFixture(t)
		read := fixture.issueWith("A long page", long, 1)
		next := fixture.issueWith("Another long page", long, 2)
		pane := fixture.model(80, 24)

		scrollDetail(t, pane)
		fixture.close(read.ID, "closed from another terminal")
		press(t, pane, "R")

		if pane.detailID != next.ID {
			t.Fatalf("detail = %s after the refresh, want %s — it did not retarget, so this cannot fail for its clause",
				pane.detailID, next.ID)
		}
		if got := pane.detail.YOffset(); got != 0 {
			t.Fatalf("y offset = %d on a page the reader never opened, want 0", got)
		}
	})
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

	press(t, pane, "R")

	if !strings.Contains(pane.pageText, "a comment from another terminal") {
		t.Fatalf("page did not pick up the new comment:\n%s", pane.pageText)
	}
}

func TestAModalCannotReachTheReadyNarrowing(t *testing.T) {
	// §Q7's argument, applied to g7b23's key: a modal captures every key, so
	// the guard is satisfied by construction and this is the assertion that
	// says so. A narrowing reached from inside a picker would re-cut the row
	// set under a reader who is looking at a relation list.
	fixture := newFixture(t)
	parent := fixture.issue("The parent", 2)
	fixture.child(parent.ID, "The child", 2)
	blocked := fixture.issue("Blocked, and hidden by `r`", 2)
	blocker := fixture.issue("The blocker", 2)
	fixture.blocks(blocked.ID, blocker.ID)
	pane := fixture.model(80, 24)

	press(t, pane, "f")
	if pane.modal == nil {
		t.Fatalf("the picker did not open; this fixture proves nothing")
	}
	before := len(pane.rows)

	press(t, pane, "r")

	if pane.modal == nil {
		t.Fatalf("`r` closed the picker")
	}
	if pane.readyOnly || len(pane.rows) != before {
		t.Fatalf("rows = %d (readyOnly %v), want `r` inside a modal to narrow nothing",
			len(pane.rows), pane.readyOnly)
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
	press(t, pane, "R")
	if pane.modal == nil {
		t.Fatalf("`R` closed the picker")
	}
	if len(pane.rows) != before {
		t.Fatalf("rows = %d, want `R` inside a modal to reach no refresh", len(pane.rows))
	}
	pressNamed(t, pane, tea.KeyEsc)

	// And the `/` prompt swallows it into the filter, which is why a filter
	// may contain an `R` without re-reading the store.
	press(t, pane, "/")
	press(t, pane, "R")
	if pane.filter != "R" {
		t.Fatalf("filter = %q, want `R` to have been typed into it", pane.filter)
	}
	if len(pane.rows) == before {
		t.Fatalf("the filter did not narrow, so this test cannot fail for its clause")
	}
}
