package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/model"
)

// esc is the universal go-back key (m3z5y). The whole ticket is the ORDERING,
// so these tests are one per layer that could be skipped or taken out of turn,
// not one per test function: a layer is asserted alone, and then against the
// layer beneath it with both live at once. A test that only ever sets up one
// layer cannot fail for a wrong order, which is this repository's dominant
// defect class.
//
// The settled order, most transient first:
//
//	modal · `/` prompt · help · follow stack · filter · zoom · nothing
//
// The first two are handled before the main keymap is reached — a modal
// captures every key, and the `/` prompt swallows them — so they are covered
// by TestEscCancelsThePickerAndLeavesTheStackAlone and
// TestTheFilterPromptSwallowsItsKeys. What escape() itself orders is the last
// four, and one press pops exactly one of them.

func escape(t *testing.T, pane *Model) {
	t.Helper()
	if cmd := pane.key(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Fatal("esc returned a command; a mistyped `/` must not drop you out of a full-screen program")
	}
	if pane.err != nil {
		t.Fatalf("esc left an error on the pane: %v", pane.err)
	}
}

func TestEscClosesTheHelpScreen(t *testing.T) {
	f := newFixture(t)
	f.issue("alpha", 2)
	pane := f.model(80, 24)

	press(t, pane, "?")
	if !pane.help {
		t.Fatal("`?` did not open the help; this fixture proves nothing")
	}
	escape(t, pane)
	if pane.help {
		t.Fatal("esc left the help screen up")
	}
}

func TestTheHelpScreenCapturesTheKeysBehindIt(t *testing.T) {
	// The help replaces the whole frame, so a key that acted behind it would
	// act invisibly — and one of them broke `esc`: `x` opened an Actions
	// modal under the help, and a modal captures input, so the `esc` meant
	// to close the help was swallowed by a picker the reader could not see.
	// That is why the help is a layer AND a capture, not just a layer.
	f := newFixture(t)
	f.issue("alpha", 1)
	pane := f.model(80, 24)

	press(t, pane, "?")
	press(t, pane, "x")
	if pane.modal != nil {
		t.Fatal("`x` opened a modal under the help; esc would close that instead of the help")
	}
	press(t, pane, "j")
	if pane.cursor.position != 0 {
		t.Fatalf("`j` moved the cursor to %d behind the help", pane.cursor.position)
	}

	escape(t, pane)
	if pane.help {
		t.Fatal("esc did not close the help")
	}
}

func TestEscClearsTheWholeFollowStackWithoutMovingTheCursor(t *testing.T) {
	// The reversal m3z5y records: `j` clears the stack, but it clears it by
	// MOVING the cursor, so before this there was no single key that
	// returned the right pane to the cursor's own issue while leaving the
	// cursor on it. Two levels deep, because one level cannot tell a
	// whole-stack pop from `backspace`.
	f := newFixture(t)
	here := f.issue("The cursor's own issue", 1)
	middle := f.issue("One level down", 2)
	bottom := f.issue("Two levels down", 2)
	f.dep(here.ID, middle.ID, model.DepBlocks)
	f.dep(middle.ID, bottom.ID, model.DepBlocks)

	pane := f.model(80, 24)
	press(t, pane, "j")
	press(t, pane, "k")
	before := pane.cursor.position
	if pane.cursor.id != here.ID {
		t.Fatalf("cursor is on %s, want %s", pane.cursor.id, here.ID)
	}

	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if len(pane.stack) != 2 || pane.detailID != bottom.ID {
		t.Fatalf("followed to %s at depth %d, want %s at depth 2",
			pane.detailID, len(pane.stack), bottom.ID)
	}

	escape(t, pane)
	if len(pane.stack) != 0 {
		t.Fatalf("stack after esc = %v, want it emptied in one press", pane.stack)
	}
	if pane.detailID != here.ID {
		t.Fatalf("detail pane shows %s after esc, want the cursor's own issue %s",
			pane.detailID, here.ID)
	}
	if pane.cursor.id != here.ID || pane.cursor.position != before {
		t.Fatalf("cursor moved to %s at %d, want %s held at %d — esc retargets the "+
			"right pane WITHOUT moving the list cursor",
			pane.cursor.id, pane.cursor.position, here.ID, before)
	}
}

func TestEscTakesTheHelpScreenBeforeTheFollowStack(t *testing.T) {
	f := newFixture(t)
	here := f.issue("The cursor's own issue", 1)
	target := f.issue("One level down", 2)
	f.dep(here.ID, target.ID, model.DepBlocks)

	pane := f.model(80, 24)
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	press(t, pane, "?")
	if !pane.help || len(pane.stack) != 1 {
		t.Fatalf("fixture is help=%v depth=%d, want both live", pane.help, len(pane.stack))
	}

	escape(t, pane)
	if pane.help {
		t.Fatal("esc did not close the help first")
	}
	if len(pane.stack) != 1 || pane.detailID != target.ID {
		t.Fatalf("the follow stack went with the help: depth %d showing %s, want depth 1 on %s",
			len(pane.stack), pane.detailID, target.ID)
	}

	escape(t, pane)
	if len(pane.stack) != 0 || pane.detailID != here.ID {
		t.Fatalf("second esc left depth %d on %s, want the stack cleared back to %s",
			len(pane.stack), pane.detailID, here.ID)
	}
}

func TestEscTakesTheFollowStackBeforeTheFilter(t *testing.T) {
	f := newFixture(t)
	here := f.issue("alpha, the cursor's own issue", 1)
	target := f.issue("bravo, one level down", 2)
	f.dep(here.ID, target.ID, model.DepBlocks)

	pane := f.model(80, 24)
	pane.filter = "alpha"
	if err := pane.applyFilter(); err != nil {
		t.Fatalf("apply filter: %v", err)
	}
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.filter == "" || len(pane.stack) != 1 {
		t.Fatalf("fixture is filter=%q depth=%d, want both live", pane.filter, len(pane.stack))
	}

	escape(t, pane)
	if len(pane.stack) != 0 || pane.detailID != here.ID {
		t.Fatalf("esc left depth %d on %s, want the stack cleared back to %s",
			len(pane.stack), pane.detailID, here.ID)
	}
	if pane.filter != "alpha" {
		t.Fatalf("the filter went with the stack: %q, want it to survive one press", pane.filter)
	}

	escape(t, pane)
	if pane.filter != "" || len(pane.rows) != 2 {
		t.Fatalf("second esc left filter %q over %v, want it cleared",
			pane.filter, ids(pane.rows))
	}
}

func TestEscUnzoomsOnlyWhenNothingElseIsLive(t *testing.T) {
	// Zoom is the LAST layer, which is what makes it safe to add a sixth
	// meaning at all: it can only fire when esc has nothing else to pop.
	f := newFixture(t)
	f.issue("alpha", 1)
	f.issue("bravo", 2)

	pane := f.model(80, 24)
	pane.filter = "alpha"
	if err := pane.applyFilter(); err != nil {
		t.Fatalf("apply filter: %v", err)
	}
	pressNamed(t, pane, tea.KeyEnter)
	if !pane.zoomed || pane.filter == "" {
		t.Fatalf("fixture is zoomed=%v filter=%q, want both live", pane.zoomed, pane.filter)
	}

	escape(t, pane)
	if pane.filter != "" {
		t.Fatalf("esc left filter %q, want the filter taken first", pane.filter)
	}
	if !pane.zoomed {
		t.Fatal("esc unzoomed while the filter was still set; zoom is the last layer")
	}

	escape(t, pane)
	if pane.zoomed {
		t.Fatal("second esc left the pane zoomed")
	}
}

func TestEscNeverQuitsWithNothingActive(t *testing.T) {
	f := newFixture(t)
	f.issue("alpha", 2)
	pane := f.model(80, 24)

	escape(t, pane)
	escape(t, pane)
	if len(pane.rows) != 1 {
		t.Fatalf("row set after two idle escapes = %v, want it untouched", ids(pane.rows))
	}
}
