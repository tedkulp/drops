package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/model"
)

// press feeds one key through the model the way bubbletea would, and hands
// back whatever command the keymap returned. Most callers want none of it —
// only `q` and `y` produce one — but a command is a func, so RUNNING it is the
// only way to see what a key sent, and returning it here is what saves the
// package a third press helper.
func press(t *testing.T, pane *Model, key string) tea.Cmd {
	t.Helper()
	cmd := pane.key(tea.KeyPressMsg{Code: keyCode(key), Text: key})
	if pane.err != nil {
		t.Fatalf("pressing %q left an error on the pane: %v", key, pane.err)
	}
	return cmd
}

// keyCode maps the one-rune keys these tests press. Named keys go through
// pressNamed, which builds the message bubbletea itself would deliver.
func keyCode(key string) rune { return []rune(key)[0] }

func TestRowSetHoldsOpenAndInProgressIssues(t *testing.T) {
	fixture := newFixture(t)
	open := fixture.issue("An open issue", 2)
	working := fixture.issue("The one being worked on", 2)
	done := fixture.issue("A finished issue", 2)
	fixture.setStatus(working.ID, model.StatusInProgress)
	fixture.setStatus(done.ID, model.StatusClosed)

	pane := fixture.model(80, 24)

	// `ready` cannot see an in_progress issue at all, which is exactly why
	// the pane lists `list`'s contract instead: it would structurally hide
	// the issue you are working on.
	if got := ids(pane.rows); len(got) != 2 {
		t.Fatalf("row set = %v, want the open and the in_progress issue", got)
	}
	if !hasID(pane.rows, working.ID) {
		t.Fatalf("row set = %v, want it to hold the in_progress issue %s", ids(pane.rows), working.ID)
	}
	if !hasID(pane.rows, open.ID) {
		t.Fatalf("row set = %v, want it to hold the open issue %s", ids(pane.rows), open.ID)
	}
	if hasID(pane.rows, done.ID) {
		t.Fatalf("row set = %v, want the closed issue absent until `C`", ids(pane.rows))
	}
}

func TestCIncludesClosedAndAWidensToEveryProject(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("An open issue", 2)
	done := fixture.issue("A finished issue", 2)
	fixture.setStatus(done.ID, model.StatusClosed)
	elsewhere := fixture.issueIn(fixture.other, "Another project's issue", 2)

	pane := fixture.model(80, 24)
	if len(pane.rows) != 1 {
		t.Fatalf("row set = %v, want one row before C and a", ids(pane.rows))
	}

	press(t, pane, "C")
	if len(pane.rows) != 2 || !hasID(pane.rows, done.ID) {
		t.Fatalf("row set after C = %v, want the closed issue included", ids(pane.rows))
	}

	press(t, pane, "a")
	if !hasID(pane.rows, elsewhere.ID) {
		t.Fatalf("row set after a = %v, want the other project's issue included", ids(pane.rows))
	}
	// The footer is styled, so this reads through the escapes rather than
	// anchoring on them.
	if footer := pane.footer(measure(80, 24, false)); !strings.Contains(footer, "all projects · 3 issues · closed") {
		t.Fatalf("footer = %q, want the widened scope, the count and the modes as words", footer)
	}
}

func TestTheFilterPersistsAcrossAAndC(t *testing.T) {
	// "Filter, then widen scope to see if it exists elsewhere" is the motion
	// `a` exists for, and it is two keystrokes only if the filter survives.
	fixture := newFixture(t)
	fixture.issue("cutover the store", 2)
	fixture.issue("something else", 2)
	fixture.issueIn(fixture.other, "cutover elsewhere", 2)

	pane := fixture.model(80, 24)
	pane.filter = "cutover"
	if err := pane.applyFilter(); err != nil {
		t.Fatalf("apply filter: %v", err)
	}
	if len(pane.rows) != 1 {
		t.Fatalf("filtered row set = %v, want one row", ids(pane.rows))
	}

	press(t, pane, "a")
	if pane.filter != "cutover" {
		t.Fatalf("filter after a = %q, want it to survive", pane.filter)
	}
	if len(pane.rows) != 2 {
		t.Fatalf("row set after a = %v, want both projects' matches", ids(pane.rows))
	}
}

func TestEscClearsTheFilterAndNeverQuits(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("alpha", 2)
	fixture.issue("bravo", 2)

	pane := fixture.model(80, 24)
	pane.filter = "alpha"
	if err := pane.applyFilter(); err != nil {
		t.Fatalf("apply filter: %v", err)
	}

	cmd := pane.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("esc returned a command; a mistyped `/` must not drop you out of a full-screen program")
	}
	if pane.filter != "" || len(pane.rows) != 2 {
		t.Fatalf("after esc: filter %q over %v, want it cleared", pane.filter, ids(pane.rows))
	}
}

func TestTheFilterPromptSwallowsItsKeys(t *testing.T) {
	// While `/` is open, `q`, `a` and `C` are characters, not commands.
	fixture := newFixture(t)
	fixture.issue("a quiet Cutover", 2)
	fixture.issue("something else", 2)

	pane := fixture.model(80, 24)
	press(t, pane, "/")
	for _, key := range []string{"C", "u", "t"} {
		press(t, pane, key)
	}
	if pane.filter != "Cut" {
		t.Fatalf("filter = %q, want %q typed into the prompt", pane.filter, "Cut")
	}
	if pane.scope.includeClosed || pane.scope.allProjects {
		t.Fatalf("scope = %#v, want the prompt to have swallowed C", pane.scope)
	}
	if len(pane.rows) != 1 {
		t.Fatalf("row set = %v, want the one case-insensitive match", ids(pane.rows))
	}
}

func assertCtrlCQuits(t *testing.T, pane *Model, mode string) {
	t.Helper()
	cmd := pane.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatalf("ctrl+c returned no command %s", mode)
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c returned %T %s, want tea.QuitMsg", msg, mode)
	}
}

func TestCtrlCQuitsFromTheTopLevel(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("An issue", 2)
	assertCtrlCQuits(t, fixture.model(80, 24), "at the top level")
}

func TestCtrlCQuitsFromAnOpenModal(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("An issue", 2)
	pane := fixture.model(80, 24)
	press(t, pane, "x")
	if pane.modal == nil {
		t.Fatal("x opened no modal; this fixture proves nothing")
	}
	assertCtrlCQuits(t, pane, "while a modal was open")
}

func TestCtrlCQuitsFromTheFilterPrompt(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("An issue", 2)
	pane := fixture.model(80, 24)
	press(t, pane, "/")
	assertCtrlCQuits(t, pane, "while the filter prompt was open")
}

func TestFollowingRetargetsTheDetailPaneWithoutMovingTheCursor(t *testing.T) {
	// The one navigational claim the whole map rests on. qy3de.12 gives it a
	// key; the mechanism is here, so it is asserted here.
	fixture := newFixture(t)
	first := fixture.issue("First", 1)
	second := fixture.issue("Second", 2)

	pane := fixture.model(80, 24)
	if pane.cursor.id != first.ID {
		t.Fatalf("cursor starts on %s, want %s", pane.cursor.id, first.ID)
	}

	if err := pane.showDetail(second.ID); err != nil {
		t.Fatalf("show detail: %v", err)
	}
	if pane.detailID != second.ID {
		t.Fatalf("detail pane shows %s, want %s", pane.detailID, second.ID)
	}
	if pane.cursor.id != first.ID || pane.cursor.position != 0 {
		t.Fatalf("cursor moved to %s/%d; retargeting the detail pane must not move it",
			pane.cursor.id, pane.cursor.position)
	}
}

func TestTheDetailPaneRendersTheIssuePage(t *testing.T) {
	fixture := newFixture(t)
	only := fixture.issue("Build the frame", 2)

	pane := fixture.model(80, 24)
	if !strings.Contains(pane.pageText, "Build the frame") {
		t.Fatalf("detail text = %q, want the issue title", pane.pageText)
	}
	if !strings.Contains(pane.pageText, string(only.ID)+" · drops · open · task · P2") {
		t.Fatalf("detail text = %q, want render's own identity line", pane.pageText)
	}
}

func TestTheBlockedMarkerCountsOpenBlockersOnly(t *testing.T) {
	fixture := newFixture(t)
	blocked := fixture.issue("Waiting on two", 2)
	first := fixture.issue("Blocker one", 2)
	second := fixture.issue("Blocker two", 2)
	fixture.blocks(blocked.ID, first.ID)
	fixture.blocks(blocked.ID, second.ID)

	pane := fixture.model(80, 24)
	if got := blockedBy(pane.rows, blocked.ID); got != 2 {
		t.Fatalf("blocked-by count = %d, want 2", got)
	}

	fixture.setStatus(first.ID, model.StatusClosed)
	if err := pane.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := blockedBy(pane.rows, blocked.ID); got != 1 {
		t.Fatalf("blocked-by count after one blocker closed = %d, want 1", got)
	}
}

func TestTheEmptyStateTellsAnEmptyProjectFromABadFilter(t *testing.T) {
	fixture := newFixture(t)
	pane := fixture.model(80, 24)

	if want := "No open issues in drops"; pane.emptyState() != want {
		t.Fatalf("empty state = %q, want %q", pane.emptyState(), want)
	}
	pane.filter = "cutover"
	if want := "No rows match cutover"; pane.emptyState() != want {
		t.Fatalf("empty state = %q, want %q", pane.emptyState(), want)
	}
}

func TestReadyComposesWithTheFilterAndSurvivesAAndC(t *testing.T) {
	// The whole reason `r` is a local predicate and not a store scope: it
	// stacks with `/`, `a` and `C` the way matching already does, and a
	// reload cannot drop it.
	fixture := newFixture(t)
	free := fixture.issue("cutover the store", 2)
	blocked := fixture.issue("cutover the blocked one", 2)
	blocker := fixture.issue("the blocker", 2)
	fixture.blocks(blocked.ID, blocker.ID)
	elsewhere := fixture.issueIn(fixture.other, "cutover elsewhere", 2)
	done := fixture.issue("cutover finished", 2)
	fixture.setStatus(done.ID, model.StatusClosed)

	pane := fixture.model(80, 24)
	press(t, pane, "r")
	pane.filter = "cutover"
	if err := pane.applyFilter(); err != nil {
		t.Fatalf("apply filter: %v", err)
	}
	if got := ids(pane.rows); len(got) != 1 || got[0] != free.ID {
		t.Fatalf("row set under `r` and /cutover = %v, want just %s", got, free.ID)
	}

	press(t, pane, "a")
	if !pane.readyOnly {
		t.Fatalf("`r` did not survive `a`; a reload must not drop a local narrowing")
	}
	if !hasID(pane.rows, elsewhere.ID) || hasID(pane.rows, blocked.ID) {
		t.Fatalf("row set after `a` = %v, want the other project's match and still no blocked row",
			ids(pane.rows))
	}

	press(t, pane, "C")
	if !hasID(pane.rows, done.ID) {
		t.Fatalf("row set after `C` = %v, want the closed match included", ids(pane.rows))
	}
	if hasID(pane.rows, blocked.ID) {
		t.Fatalf("row set after `C` = %v, want `r` still hiding the blocked row", ids(pane.rows))
	}
}

func TestReadyNarrowingSurvivesARefresh(t *testing.T) {
	// reload is not the only way rows are re-derived: `R` and every write go
	// through refresh, which builds m.rows on its own. Both paths have to ask
	// the same question, or an agent writing a blocked issue puts it on a
	// pane the reader has told to hide exactly that.
	fixture := newFixture(t)
	fixture.issue("Something you can pick up", 2)
	pane := fixture.model(80, 24)
	press(t, pane, "r")

	blocked := fixture.issue("Written by somebody else", 2)
	blocker := fixture.issue("And its blocker", 2)
	fixture.blocks(blocked.ID, blocker.ID)

	press(t, pane, "R")

	if !hasID(pane.rows, blocker.ID) {
		t.Fatalf("row set after `R` = %v, want the new unblocked issue read in", ids(pane.rows))
	}
	if hasID(pane.rows, blocked.ID) {
		t.Fatalf("row set after `R` = %v, want %s still hidden — `r` is still on",
			ids(pane.rows), blocked.ID)
	}
}

func TestTheFooterDisclosesReadyAsAWord(t *testing.T) {
	// qy3de.5's rule: modes read as WORDS, because `r` means nothing to
	// somebody who did not press it. Beside `closed`, `wrap` and `zoom
	// detail`, and with the count going `M of N` while it narrows.
	fixture := newFixture(t)
	fixture.issue("Something you can pick up", 2)
	blocked := fixture.issue("Something you cannot", 2)
	blocker := fixture.issue("The blocker", 2)
	fixture.blocks(blocked.ID, blocker.ID)

	pane := fixture.model(80, 24)
	if strings.Contains(pane.footer(pane.geo()), "ready") {
		t.Fatalf("footer = %q, want no mode word before `r`", pane.footer(pane.geo()))
	}

	press(t, pane, "r")

	if footer := pane.footer(pane.geo()); !strings.Contains(footer, "drops · 2 of 3 · ready") {
		t.Fatalf("footer = %q, want the narrowed count and the mode as a word", footer)
	}
}

func TestAnOverflowingFooterClosesItsDimStyle(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("Something you can pick up", 2)
	pane := fixture.model(30, 24)
	pane.filter = strings.Repeat("界", 30)

	footer := pane.footer(pane.geo())

	if !strings.HasSuffix(footer, "\x1b[m") && !strings.HasSuffix(footer, "\x1b[0m") {
		t.Fatalf("overflowing footer = %q, want its trailing SGR reset", footer)
	}
}

func TestRHidesTheBlockedRowsAndPressingItAgainBringsThemBack(t *testing.T) {
	fixture := newFixture(t)
	free := fixture.issue("Something you can pick up", 2)
	blocked := fixture.issue("Something you cannot", 2)
	blocker := fixture.issue("The blocker", 2)
	fixture.blocks(blocked.ID, blocker.ID)

	pane := fixture.model(80, 24)
	if len(pane.rows) != 3 {
		t.Fatalf("row set at rest = %v, want all three", ids(pane.rows))
	}

	press(t, pane, "r")

	if hasID(pane.rows, blocked.ID) {
		t.Fatalf("row set under `r` = %v, want %s gone — it has an open blocker", ids(pane.rows), blocked.ID)
	}
	if !hasID(pane.rows, free.ID) || !hasID(pane.rows, blocker.ID) {
		t.Fatalf("row set under `r` = %v, want the two rows with no open blocker", ids(pane.rows))
	}

	press(t, pane, "r")

	if !hasID(pane.rows, blocked.ID) {
		t.Fatalf("row set after `r` again = %v, want %s back — the key is a toggle",
			ids(pane.rows), blocked.ID)
	}
}

func hasID(rows []row, id model.ID) bool {
	for _, candidate := range rows {
		if candidate.id == id {
			return true
		}
	}
	return false
}

func blockedBy(rows []row, id model.ID) int {
	for _, candidate := range rows {
		if candidate.id == id {
			return candidate.blockedBy
		}
	}
	return -1
}
