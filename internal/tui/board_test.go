package tui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/model"
)

// columnIDs reads the board back as ids per column, which is what column
// membership is asserted through.
func columnIDs(pane *Model) [columnCount][]model.ID {
	var out [columnCount][]model.ID
	for index, cards := range pane.cards {
		out[index] = ids(cards)
	}
	return out
}

// onBoard builds the navigator and switches it to the board.
func onBoard(t *testing.T, f *fixture, width, height int) *Model {
	t.Helper()
	pane := f.model(width, height)
	press(t, pane, "B")
	if !pane.board {
		t.Fatal("B did not switch to the board")
	}
	return pane
}

// The four derivation clauses, one fixture: an open issue with no open blocker
// is Ready, an open issue with one is Blocked, an in_progress issue is In
// progress EVEN WHEN BLOCKED, and a closed issue is Closed. The in_progress
// issue is deliberately blocked, because that is the one where two rules
// disagree and the order of the branches decides.
func TestTheBoardSetsEachIssueInItsDerivedColumn(t *testing.T) {
	fixture := newFixture(t, "ready", "waiting", "doing", "done", "wall")
	fixture.issue("ready", 2)
	fixture.issue("waiting", 2)
	fixture.issue("doing", 2)
	fixture.issue("done", 2)
	fixture.issue("wall", 2)
	fixture.blocks("waiting", "wall")
	fixture.blocks("doing", "wall")
	fixture.setStatus("doing", model.StatusInProgress)
	fixture.close("done", "finished")

	pane := onBoard(t, fixture, 200, 50)

	want := [columnCount][]model.ID{
		colReady:      {"wall", "ready"},
		colBlocked:    {"waiting"},
		colInProgress: {"doing"},
		colClosed:     {"done"},
	}
	if got := columnIDs(pane); !equalColumns(got, want) {
		t.Fatalf("columns = %v, want %v", got, want)
	}
}

func equalColumns(got, want [columnCount][]model.ID) bool {
	for index := range got {
		if !slices.Equal(got[index], want[index]) {
			return false
		}
	}
	return true
}

// shiftTab presses shift+tab, which is a modified named key rather than a rune.
func shiftTab(t *testing.T, pane *Model) {
	t.Helper()
	pane.key(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if pane.err != nil {
		t.Fatalf("shift+tab left an error on the pane: %v", pane.err)
	}
}

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain is a frame with its styling taken off, so a test can read the words.
func plain(frame string) string { return sgr.ReplaceAllString(frame, "") }

// The clause: Closed is core's order — most recently closed first — and the
// board never re-sorts it or takes it from anywhere else. The close order is
// chosen to disagree with id order and with the queue's priority order, and
// the case under `C` is the one that can go wrong: the list then holds the
// same closed issues in the QUEUE's order.
func TestTheBoardTakesClosedInCoresOrder(t *testing.T) {
	for _, withClosed := range []bool{false, true} {
		name := "C off"
		if withClosed {
			name = "C on"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t, "alpha", "bravo", "charlie")
			fixture.issue("alpha", 1)
			fixture.issue("bravo", 3)
			fixture.issue("charlie", 2)
			fixture.close("bravo", "first")
			fixture.close("alpha", "second")
			fixture.close("charlie", "third")

			pane := fixture.model(200, 50)
			if withClosed {
				press(t, pane, "C")
			}
			press(t, pane, "B")

			if got, want := ids(pane.cards[colClosed]), []model.ID{"charlie", "alpha", "bravo"}; !slices.Equal(got, want) {
				t.Fatalf("Closed = %v, want %v: most recently closed first, as core ordered it", got, want)
			}
			if footer := pane.footer(pane.geo()); !strings.Contains(footer, "drops · 3 issues · board") {
				t.Fatalf("footer = %q, want each closed issue counted once", footer)
			}
		})
	}
}

// The clause: `B` reads nothing and carries everything that narrows or
// trails. The store is CLOSED before the key is pressed, so any read at all
// leaves an error on the pane and fails the press.
func TestBReadsNothingAndCarriesTheCursorFilterScopeReadyAndTrail(t *testing.T) {
	fixture := newFixture(t, "ready", "doing", "notes")
	fixture.issue("ready to go", 1)
	fixture.issue("doing it", 2)
	fixture.issue("notes on it", 3)
	fixture.setStatus("doing", model.StatusInProgress)
	fixture.dep("doing", "notes", model.DepRelated)

	pane := fixture.model(120, 40)
	press(t, pane, "a")
	press(t, pane, "r")
	for _, key := range []string{"/", "d", "o", "i", "n", "g"} {
		press(t, pane, key)
	}
	pressNamed(t, pane, tea.KeyEnter)
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.cursor.id != "doing" || pane.detailID != "notes" || len(pane.stack) != 1 {
		t.Fatalf("cursor %s, detail %s, stack %v: the fixture did not follow a relation",
			pane.cursor.id, pane.detailID, pane.stack)
	}

	if err := fixture.opened.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}
	if _, err := fixture.core.State(t.Context()); err == nil {
		t.Fatal("a closed store still reads, so this test cannot see a read")
	}

	for _, board := range []bool{true, false} {
		press(t, pane, "B")
		if pane.board != board {
			t.Fatalf("board = %v after B, want %v", pane.board, board)
		}
		if pane.cursor.id != "doing" {
			t.Fatalf("cursor = %s, want doing carried across", pane.cursor.id)
		}
		if pane.filter != "doing" || !pane.scope.allProjects || !pane.readyOnly {
			t.Fatalf("filter %q, all projects %v, ready %v: want all three carried",
				pane.filter, pane.scope.allProjects, pane.readyOnly)
		}
		if !slices.Equal(pane.stack, []model.ID{"doing"}) || pane.detailID != "notes" {
			t.Fatalf("stack %v, detail %s: want the follow trail carried", pane.stack, pane.detailID)
		}
	}
}

// Going back to the list from a card the list does not show moves the cursor,
// so the follow trail goes with it and the right pane follows the cursor, as
// for every other cursor move.
func TestLeavingTheBoardFromACardTheListLacksDropsTheTrail(t *testing.T) {
	fixture := newFixture(t, "first", "second", "done")
	fixture.issue("first", 1)
	fixture.issue("second", 2)
	fixture.issue("done", 3)
	fixture.close("done", "finished")
	fixture.dep("done", "second", model.DepRelated)

	pane := onBoard(t, fixture, 200, 50)
	shiftTab(t, pane)
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.cursor.id != "done" || pane.detailID != "second" || len(pane.stack) != 1 {
		t.Fatalf("cursor %s, detail %s, stack %v: the fixture did not follow out of Closed",
			pane.cursor.id, pane.detailID, pane.stack)
	}

	press(t, pane, "B")
	if pane.cursor.id != "first" || pane.detailID != "first" || len(pane.stack) != 0 {
		t.Fatalf("back in the list: cursor %s, detail %s, stack %v; want first, first, and no trail",
			pane.cursor.id, pane.detailID, pane.stack)
	}
}

// The clause: the cursor's issue takes the focus to its column. The issue is
// not in Ready, so a board that opened on its first column would put the
// cursor on some other card.
func TestBFocusesTheColumnHoldingTheCursorsIssue(t *testing.T) {
	fixture := newFixture(t, "ready", "waiting", "wall")
	fixture.issue("ready", 1)
	fixture.issue("waiting", 2)
	fixture.issue("wall", 3)
	fixture.blocks("waiting", "wall")

	pane := fixture.model(200, 50)
	press(t, pane, "j")
	if pane.cursor.id != "waiting" {
		t.Fatalf("list cursor = %s, want waiting", pane.cursor.id)
	}
	press(t, pane, "B")
	if pane.focus != colBlocked || pane.cursor.id != "waiting" || pane.detailID != "waiting" {
		t.Fatalf("focus %d, cursor %s, detail %s: want Blocked, waiting, waiting",
			pane.focus, pane.cursor.id, pane.detailID)
	}
}

// `Esc` pops what it always pops and never the board, and `b` is still the
// detail pane's page-up rather than anything to do with `B`.
func TestEscNeverLeavesTheBoardAndBStillPagesTheDetailPane(t *testing.T) {
	fixture := newFixture(t)
	fixture.issueWith("A long page", strings.Repeat("A line worth paging past.\n\n", 80), 2)
	pane := onBoard(t, fixture, 120, 30)

	pane.filter = "long"
	if err := pane.applyFilter(); err != nil {
		t.Fatalf("filter: %v", err)
	}
	for range 4 {
		pressNamed(t, pane, tea.KeyEscape)
	}
	if !pane.board {
		t.Fatal("Esc left the board")
	}
	if pane.filter != "" {
		t.Fatalf("filter = %q, want Esc to have popped it as it does in the list", pane.filter)
	}

	press(t, pane, "space")
	if pane.detail.YOffset() == 0 {
		t.Fatal("space did not page the detail pane down, so b cannot be seen paging up")
	}
	press(t, pane, "b")
	if pane.detail.YOffset() != 0 || !pane.board {
		t.Fatalf("after b: y offset %d, board %v; want the page back at the top and the board still up",
			pane.detail.YOffset(), pane.board)
	}
}

// j k g G move within the focused column only, retarget the right pane, and
// clear the follow trail as the list cursor does.
func TestMovementStaysInTheFocusedColumnAndClearsTheTrail(t *testing.T) {
	fixture := newFixture(t, "one", "two", "three", "waiting")
	fixture.issue("one", 1)
	fixture.issue("two", 1)
	fixture.issue("three", 1)
	fixture.issue("waiting", 2)
	fixture.blocks("waiting", "one")

	pane := onBoard(t, fixture, 200, 50)
	if got := ids(pane.cards[colReady]); !slices.Equal(got, []model.ID{"three", "two", "one"}) {
		t.Fatalf("Ready = %v, want three two one", got)
	}
	press(t, pane, "G")
	if pane.cursor.id != "one" || pane.focus != colReady {
		t.Fatalf("G put the cursor on %s in column %d, want one in Ready", pane.cursor.id, pane.focus)
	}
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if len(pane.stack) != 1 {
		t.Fatal("the fixture did not follow a relation")
	}
	pressNamed(t, pane, tea.KeyTab)
	if len(pane.stack) != 0 {
		t.Fatalf("tab left the follow trail %v; moving to another column moves the cursor", pane.stack)
	}
	shiftTab(t, pane)
	press(t, pane, "G")
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	press(t, pane, "k")
	if pane.cursor.id != "two" || pane.detailID != "two" || len(pane.stack) != 0 {
		t.Fatalf("after k: cursor %s, detail %s, stack %v; want two, two, and no trail",
			pane.cursor.id, pane.detailID, pane.stack)
	}
	press(t, pane, "j")
	press(t, pane, "j")
	if pane.cursor.id != "one" {
		t.Fatalf("j past the end of Ready = %s, want it held on one, never into Blocked", pane.cursor.id)
	}
	press(t, pane, "g")
	if pane.cursor.id != "three" {
		t.Fatalf("g = %s, want three", pane.cursor.id)
	}
}

// The clause: `tab` lands on the card at the old row position ON SCREEN,
// clamped to the target column, and `tab` and `shift+tab` wrap round all four.
func TestTabLandsOnTheNearestRowPositionAndWraps(t *testing.T) {
	fixture := newFixture(t, "r1", "r2", "r3", "r4", "b1", "b2", "b3", "done")
	// Ready's issues outrank Blocked's, so the list — and so the board's
	// first focus — opens in Ready.
	for _, title := range []string{"r1", "r2", "r3", "r4"} {
		fixture.issue(title, 1)
	}
	for _, title := range []string{"b1", "b2", "b3", "done"} {
		fixture.issue(title, 2)
	}
	for _, blocked := range []model.ID{"b1", "b2", "b3"} {
		fixture.blocks(blocked, "r1")
	}
	fixture.close("done", "finished")

	pane := onBoard(t, fixture, 200, 50)
	press(t, pane, "j")
	press(t, pane, "j")
	pressNamed(t, pane, tea.KeyTab)
	if pane.focus != colBlocked || pane.cursor.position != 2 {
		t.Fatalf("tab from Ready row 2 = column %d row %d, want Blocked row 2", pane.focus, pane.cursor.position)
	}
	if want := pane.cards[colBlocked][2].id; pane.cursor.id != want || pane.detailID != want {
		t.Fatalf("cursor %s, detail %s, want both on %s", pane.cursor.id, pane.detailID, want)
	}

	shiftTab(t, pane)
	press(t, pane, "G")
	pressNamed(t, pane, tea.KeyTab)
	if pane.focus != colBlocked || pane.cursor.position != 2 {
		t.Fatalf("tab from Ready row 3 = column %d row %d, want Blocked's last row, 2", pane.focus, pane.cursor.position)
	}

	pressNamed(t, pane, tea.KeyTab)
	pressNamed(t, pane, tea.KeyTab)
	if pane.focus != colClosed {
		t.Fatalf("two more tabs = column %d, want Closed", pane.focus)
	}
	pressNamed(t, pane, tea.KeyTab)
	if pane.focus != colReady {
		t.Fatalf("tab from Closed = column %d, want it to wrap to Ready", pane.focus)
	}
	shiftTab(t, pane)
	if pane.focus != colClosed || pane.cursor.id != "done" {
		t.Fatalf("shift+tab from Ready = column %d on %s, want it to wrap to Closed on done", pane.focus, pane.cursor.id)
	}
}

// The row position is the position ON SCREEN: a column scrolled down lands on
// the same visible row of the next one, not on the same index.
func TestTabCountsTheRowPositionOnScreen(t *testing.T) {
	fixture := newFixture(t, "r1", "r2", "r3", "r4", "b1", "b2", "b3")
	// Ready's issues outrank Blocked's, so the list — and so the board's
	// first focus — opens in Ready.
	for _, title := range []string{"r1", "r2", "r3", "r4"} {
		fixture.issue(title, 1)
	}
	for _, title := range []string{"b1", "b2", "b3"} {
		fixture.issue(title, 2)
	}
	for _, blocked := range []model.ID{"b1", "b2", "b3"} {
		fixture.blocks(blocked, "r1")
	}

	// 12 lines is 9 pane rows: a heading and two cards.
	pane := onBoard(t, fixture, 200, 12)
	if fit := cardsFit(pane.geo().rows); fit != 2 {
		t.Fatalf("a column fits %d cards, want 2 for this test to scroll", fit)
	}
	press(t, pane, "G")
	pressNamed(t, pane, tea.KeyTab)
	if pane.cursor.position != 1 {
		t.Fatalf("tab from the bottom card on screen = row %d, want 1, the bottom card on screen", pane.cursor.position)
	}
}

// The clauses: as many columns show as fit at 28 wide — one at 80, two at 120,
// four at 200 — and the focused column is scrolled into view from whichever
// side it went off, with the border saying how many are hidden each way.
func TestTheBoardPagesColumnsToKeepTheFocusedOneOnScreen(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("an issue", 2)

	headings := func(pane *Model) []string {
		var found []string
		frame := plain(pane.frame())
		for _, name := range columnNames {
			if strings.Contains(frame, name+" ") {
				found = append(found, name)
			}
		}
		return found
	}

	for _, size := range []struct {
		width int
		want  []string
	}{
		{80, []string{"Ready"}},
		{120, []string{"Ready", "Blocked"}},
		{200, []string{"Ready", "Blocked", "In progress", "Closed"}},
	} {
		pane := onBoard(t, fixture, size.width, 30)
		if got := headings(pane); !slices.Equal(got, size.want) {
			t.Fatalf("at %d columns the board shows %v, want %v", size.width, got, size.want)
		}
	}

	pane := onBoard(t, fixture, 80, 30)
	if !strings.Contains(plain(pane.frame()), "drops · ◀ 0 · 3 ▶") {
		t.Fatalf("border does not say three columns are hidden to the right:\n%s", plain(pane.frame()))
	}
	shiftTab(t, pane)
	if got := headings(pane); !slices.Equal(got, []string{"Closed"}) || pane.first != 3 {
		t.Fatalf("shift+tab to Closed shows %v from column %d, want Closed from 3: scrolled in from the right", got, pane.first)
	}
	if !strings.Contains(plain(pane.frame()), "drops · ◀ 3 · 0 ▶") {
		t.Fatalf("border does not say three columns are hidden to the left:\n%s", plain(pane.frame()))
	}
	pressNamed(t, pane, tea.KeyTab)
	if got := headings(pane); !slices.Equal(got, []string{"Ready"}) || pane.first != 0 {
		t.Fatalf("tab back to Ready shows %v from column %d, want Ready from 0: scrolled in from the left", got, pane.first)
	}
}

// The clause: `▸` marks the focused column's heading and no other.
func TestOnlyTheFocusedColumnIsMarked(t *testing.T) {
	fixture := newFixture(t)
	fixture.issue("an issue", 2)
	pane := onBoard(t, fixture, 200, 30)
	pressNamed(t, pane, tea.KeyTab)

	frame := plain(pane.frame())
	if strings.Count(frame, "▸") != 1 || !strings.Contains(frame, "▸ Blocked 0") {
		t.Fatalf("want exactly one marker, on Blocked:\n%s", frame)
	}
	if !strings.Contains(frame, "  Ready 1") {
		t.Fatalf("want Ready's heading unmarked:\n%s", frame)
	}
}

// stamp is a timestamp some duration before the fixture's clock.
func stamp(before time.Duration) *model.Timestamp {
	at := model.NewTimestamp(time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).Add(-before))
	return &at
}

// A card, byte for byte, at a width that cuts. The clauses under it: the age
// counts from a moment that depends on the column, and each part of line 3 is
// there only when the card has it.
func TestACardIsThreeLinesByteForByte(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	base := row{
		id: "k3f9x", status: model.StatusOpen, priority: 1, kind: model.TypeTask,
		title:    "Cut the board over to the store's closed read",
		assignee: "ted", blockedBy: 2,
		created: stamp(3 * time.Hour), started: stamp(5 * 24 * time.Hour), closedAt: stamp(20 * time.Minute),
	}

	got := card(base, colBlocked, 30, now)
	want := [cardLines]string{
		"○ P1 task k3f9x             3h",
		"Cut the board over to the sto…",
		"@ted · blocked by 2",
	}
	if got != want {
		t.Fatalf("card =\n%q\nwant\n%q", got, want)
	}

	unstarted := base
	unstarted.started = nil
	if line := card(unstarted, colInProgress, 30, now)[0]; !strings.HasSuffix(line, " 3h") {
		t.Fatalf("In progress card with no start = %q, want its age from creation, 3h", line)
	}

	for _, testcase := range []struct {
		col  column
		want string
	}{
		{colReady, "3h"},
		{colBlocked, "3h"},
		{colInProgress, "5d"},
		{colClosed, "20m"},
	} {
		line := card(base, testcase.col, 30, now)[0]
		if !strings.HasSuffix(line, " "+testcase.want) {
			t.Fatalf("%s card line 1 = %q, want the age %s", columnNames[testcase.col], line, testcase.want)
		}
	}

	for _, testcase := range []struct {
		name      string
		assignee  string
		blockedBy int
		want      string
	}{
		{"both", "ted", 2, "@ted · blocked by 2"},
		{"unclaimed", "", 2, "blocked by 2"},
		{"unblocked", "ted", 0, "@ted"},
		{"neither", "", 0, ""},
	} {
		candidate := base
		candidate.assignee, candidate.blockedBy = testcase.assignee, testcase.blockedBy
		if line := card(candidate, colReady, 30, now)[2]; line != testcase.want {
			t.Fatalf("%s: line 3 = %q, want %q", testcase.name, line, testcase.want)
		}
	}
}

// A heading is the column's name and its count, byte for byte, with `▸ ` on
// the focused column and two spaces on any other.
func TestAHeadingIsTheNameAndCount(t *testing.T) {
	if got, want := heading(colInProgress, 12, 0, 12, true), "▸ In progress 12"; got != want {
		t.Fatalf("focused heading = %q, want %q", got, want)
	}
	if got, want := heading(colClosed, 0, 0, 3, false), "  Closed 0"; got != want {
		t.Fatalf("heading = %q, want %q", got, want)
	}
}

// The clause: only while a column overflows does its heading carry the range
// on screen, 1-based and joined with an en dash. A column that exactly fits
// is the boundary: every card is on screen, so there is no range to report.
func TestAHeadingShowsTheRangeOnlyWhileTheColumnOverflows(t *testing.T) {
	for _, testcase := range []struct {
		name            string
		count, top, fit int
		want            string
	}{
		{"scrolled deep", 300, 38, 3, "▸ Closed 300 · 39–41"},
		{"at the top", 4, 0, 3, "▸ Closed 4 · 1–3"},
		{"exactly fits", 3, 0, 3, "▸ Closed 3"},
		{"room to spare", 1, 0, 3, "▸ Closed 1"},
	} {
		if got := heading(colClosed, testcase.count, testcase.top, testcase.fit, true); got != testcase.want {
			t.Fatalf("%s: heading = %q, want %q", testcase.name, got, testcase.want)
		}
	}
}

// The clause: a column taller than the frame scrolls one card at a time as the
// cursor walks down it, and the cursor's card is always on screen. Read off
// the board body alone, because the detail pane also shows the cursor's title.
func TestALongColumnScrollsACardAtATimeKeepingTheCursorsCardOnScreen(t *testing.T) {
	titles := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf"}
	fixture := newFixture(t)
	for _, title := range titles {
		fixture.issue(title, 2)
	}

	// 12 lines is 9 pane rows: a heading and two cards.
	pane := onBoard(t, fixture, 200, 12)
	fit := cardsFit(pane.geo().rows)
	if fit != 2 {
		t.Fatalf("a column fits %d cards, want 2 for this test to scroll", fit)
	}

	for step := range titles {
		if step > 0 {
			press(t, pane, "j")
		}
		if pane.cursor.position != step {
			t.Fatalf("after %d presses the cursor is on card %d", step, pane.cursor.position)
		}
		selected := pane.cards[colReady][step].title
		geo := pane.geo()
		body := plain(pane.boardBody(geo.listWidth, geo.rows))
		first := max(0, step-fit+1)
		wantHeading := fmt.Sprintf("▸ Ready %d · %d–%d", len(titles), first+1, first+fit)
		if !strings.Contains(body, wantHeading) {
			t.Fatalf("cursor on %s: want the heading %q:\n%s", selected, wantHeading, body)
		}
		for index, candidate := range pane.cards[colReady] {
			if shown := index >= first && index < first+fit; strings.Contains(body, candidate.title) != shown {
				t.Fatalf("cursor on %s: card %s on screen = %v, want %v:\n%s",
					selected, candidate.title, !shown, shown, body)
			}
		}
	}
}

// The clauses: an empty column keeps its heading and says so under it, dimmed.
// Closed says `nothing closed`, and every other column says `none`. A column
// with cards gets no hint above them.
func TestAnEmptyColumnKeepsItsHeadingAndShowsAHint(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	for _, testcase := range []struct {
		col  column
		hint string
	}{
		{colReady, "none"},
		{colBlocked, "none"},
		{colInProgress, "none"},
		{colClosed, "nothing closed"},
	} {
		lines := strings.Split(columnBody(testcase.col, nil, -1, 28, 8, now), "\n")
		wantHeading := fmt.Sprintf("  %-26s", columnNames[testcase.col]+" 0")
		if got := plain(lines[0]); got != wantHeading {
			t.Fatalf("%s: heading line = %q, want %q", columnNames[testcase.col], got, wantHeading)
		}
		wantHint := fmt.Sprintf("%-28s", testcase.hint)
		if got := plain(lines[1]); got != wantHint {
			t.Fatalf("%s: hint line = %q, want %q", columnNames[testcase.col], got, wantHint)
		}
		if lines[1] == plain(lines[1]) {
			t.Fatalf("%s: hint line %q is unstyled, want it dimmed", columnNames[testcase.col], lines[1])
		}
	}

	occupied := row{id: "k3f9x", status: model.StatusOpen, priority: 1, kind: model.TypeTask, title: "a card"}
	lines := strings.Split(plain(columnBody(colReady, []row{occupied}, 0, 28, 8, now)), "\n")
	if !strings.HasPrefix(lines[1], "○ P1 task k3f9x") {
		t.Fatalf("a column with a card: line under the heading = %q, want the card", lines[1])
	}
}

// Every key that acts on the detail pane in the list acts on it on the board.
func TestTheDetailPaneKeysWorkOnTheBoard(t *testing.T) {
	fixture := newFixture(t, "long", "other")
	fixture.issueWith("A long page", strings.Repeat("A line worth scrolling past.\n\n", 60)+wideRow, 2)
	fixture.issue("other", 3)
	fixture.dep("long", "other", model.DepRelated)
	pane := onBoard(t, fixture, 120, 30)

	press(t, pane, "J")
	if pane.detail.YOffset() != 1 {
		t.Fatalf("J: y offset %d, want 1", pane.detail.YOffset())
	}
	press(t, pane, "K")
	press(t, pane, "l")
	if pane.detail.XOffset() != hscrollStep {
		t.Fatalf("l: x offset %d, want %d", pane.detail.XOffset(), hscrollStep)
	}
	press(t, pane, "0")
	if pane.detail.XOffset() != 0 {
		t.Fatalf("0: x offset %d, want 0", pane.detail.XOffset())
	}
	press(t, pane, "w")
	if !pane.detail.SoftWrap {
		t.Fatal("w did not wrap")
	}
	pressNamed(t, pane, tea.KeyEnter)
	if !pane.zoomed || strings.Contains(plain(pane.frame()), "Ready 2") {
		t.Fatal("enter did not zoom the detail pane over the board")
	}
	pressNamed(t, pane, tea.KeyEnter)
	if cmd := press(t, pane, "y"); cmd == nil || pane.notice != "sent long to the clipboard" {
		t.Fatalf("y: notice %q, want the id sent", pane.notice)
	}
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.detailID != "other" {
		t.Fatalf("f enter: detail %s, want other", pane.detailID)
	}
	pressNamed(t, pane, tea.KeyBackspace)
	if pane.detailID != "long" {
		t.Fatalf("backspace: detail %s, want long", pane.detailID)
	}
	press(t, pane, "x")
	if pane.modal == nil || pane.kind != modalActions {
		t.Fatal("x opened no actions modal on the board")
	}
}

// The clause: an age reads in the unit its bucket names, and each bucket ends
// exactly where the contract says — an hour, two days, sixty days, a year.
func TestAnAgeChangesUnitAtEachBoundary(t *testing.T) {
	const day = 24 * time.Hour
	for _, testcase := range []struct {
		elapsed time.Duration
		want    string
	}{
		{59 * time.Minute, "59m"},
		{time.Hour, "1h"},
		{47 * time.Hour, "47h"},
		{48 * time.Hour, "2d"},
		{59 * day, "59d"},
		{60 * day, "2mo"},
		{364 * day, "12mo"},
		{365 * day, "1y"},
	} {
		if got := age(testcase.elapsed); got != testcase.want {
			t.Fatalf("age(%v) = %q, want %q", testcase.elapsed, got, testcase.want)
		}
	}
}

// The clause: on the board the detail pane takes 40% of the frame, borders
// included, and never less than 38; the board has what is left.
func TestTheBoardLeavesTheDetailPaneFortyPercentButNeverUnder38(t *testing.T) {
	for _, testcase := range []struct {
		width, board, detail int
	}{
		{80, 40, 36},
		{120, 70, 46},
		{200, 118, 78},
	} {
		geo := measureBoard(testcase.width, 24)
		if geo.listWidth != testcase.board || geo.detailWidth != testcase.detail {
			t.Fatalf("at %d: board %d, detail %d; want %d and %d",
				testcase.width, geo.listWidth, geo.detailWidth, testcase.board, testcase.detail)
		}
	}
}
