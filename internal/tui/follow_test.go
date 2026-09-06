package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// pressNamed feeds a key bubbletea names rather than spells — backspace,
// enter, esc — as the message bubbletea itself would deliver.
func pressNamed(t *testing.T, pane *Model, code rune) {
	t.Helper()
	if cmd := pane.key(tea.KeyPressMsg{Code: code}); cmd != nil {
		_ = cmd
	}
	if pane.err != nil {
		t.Fatalf("pressing %v left an error on the pane: %v", code, pane.err)
	}
}

// grouped reads the picker's choices back as group → ids, which is what the
// direction of every edge is asserted through.
func grouped(choices []followChoice) map[string][]model.ID {
	out := map[string][]model.ID{}
	for _, choice := range choices {
		out[choice.group] = append(out[choice.group], choice.id)
	}
	return out
}

func viewOf(t *testing.T, f *fixture, id model.ID) core.IssueView {
	t.Helper()
	issue, err := f.core.ViewIssue(t.Context(), id)
	if err != nil {
		t.Fatalf("view %s: %v", id, err)
	}
	return issue
}

func TestTheFollowPickerNamesEveryDirectionFromTheReadersEnd(t *testing.T) {
	// The defect this shape produces is a swapped from/to, and it survives a
	// round-trip test: the edge is written and read back the same way round
	// whichever end the heading names. So every direction is asserted against
	// a fixture whose two ends mean different things.
	f := newFixture(t)
	here := f.issue("The issue the picker is opened on", 1)
	blocker := f.issue("What blocks it", 2)
	blocked := f.issue("What it blocks", 2)
	origin := f.issue("Where it was discovered from", 2)
	derived := f.issue("What was discovered from it", 2)
	peerOut := f.issue("A peer it points at", 2)
	peerIn := f.issue("A peer that points at it", 2)

	f.dep(here.ID, blocker.ID, model.DepBlocks)
	f.dep(blocked.ID, here.ID, model.DepBlocks)
	f.dep(here.ID, origin.ID, model.DepDiscoveredFrom)
	f.dep(derived.ID, here.ID, model.DepDiscoveredFrom)
	f.dep(here.ID, peerOut.ID, model.DepRelated)
	f.dep(peerIn.ID, here.ID, model.DepRelated)

	_, choices := followRows(viewOf(t, f, here.ID))
	got := grouped(choices)

	for _, want := range []struct {
		group string
		ids   []model.ID
	}{
		{"Blocked by", []model.ID{blocker.ID}},
		{"Blocks", []model.ID{blocked.ID}},
		{"Discovered from", []model.ID{origin.ID}},
		{"Discovered", []model.ID{derived.ID}},
		// Related is undirected, so it is ONE group merging both ways, out
		// edges first. The corpus holds a single live `related` edge.
		{"Related", []model.ID{peerOut.ID, peerIn.ID}},
	} {
		if !equalIDs(got[want.group], want.ids) {
			t.Errorf("%s = %v, want %v", want.group, got[want.group], want.ids)
		}
	}
}

// TestTheFollowPickerListsAReciprocalPairOnce: the picker reads the same
// merged relation the page does, so a `related` edge stored in both directions
// is one row and one choice. Concatenating the halves gave a duplicate row and
// two followChoice entries for one issue (y7f6z).
func TestTheFollowPickerListsAReciprocalPairOnce(t *testing.T) {
	f := newFixture(t)
	here := f.issue("The issue the picker is opened on", 1)
	peer := f.issue("A peer both machines related it to", 2)
	f.reciprocal(here.ID, peer.ID)

	rows, choices := followRows(viewOf(t, f, here.ID))
	if got := grouped(choices)["Related"]; !equalIDs(got, []model.ID{peer.ID}) {
		t.Fatalf("Related = %v, want %v once", got, []model.ID{peer.ID})
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want the one relation this issue has: %+v", len(rows), rows)
	}
}

func TestTheFollowPickerGroupsInThePagesOwnOrder(t *testing.T) {
	f := newFixture(t)
	parent := f.issue("The parent", 1)
	here := f.child(parent.ID, "The issue the picker is opened on", 1)
	child := f.child(here.ID, "A child", 2)
	blocker := f.issue("What blocks it", 2)
	blocked := f.issue("What it blocks", 2)
	origin := f.issue("Where it came from", 2)
	derived := f.issue("What came out of it", 2)
	peer := f.issue("A peer", 2)

	f.dep(here.ID, blocker.ID, model.DepBlocks)
	f.dep(blocked.ID, here.ID, model.DepBlocks)
	f.dep(here.ID, origin.ID, model.DepDiscoveredFrom)
	f.dep(derived.ID, here.ID, model.DepDiscoveredFrom)
	f.dep(here.ID, peer.ID, model.DepRelated)

	rows, choices := followRows(viewOf(t, f, here.ID))
	want := []string{"Parent", "Blocked by", "Blocks", "Children",
		"Discovered from", "Discovered", "Related"}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.Group)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("groups = %v, want %v — the picker reuses the page's words in the page's order", got, want)
	}
	if choices[3].id != child.ID {
		t.Fatalf("the Children row points at %s, want %s", choices[3].id, child.ID)
	}
}

func TestTheIDColumnAutoSizesToTheWidestIDInThatPicker(t *testing.T) {
	// gitops-vn4 is the corpus fixture: 16 live children, SEVEN of which
	// collapse to the same `gitops-vn4.…` under the left pane's twelve-column
	// cap, in a picker whose whole job is choosing among them. The picker does
	// not inherit that cap — qy3de.4's never-truncate-an-identifier exception
	// held there because `show` was one keypress away, and it fails here
	// because the picker IS the identification step.
	ids := []model.ID{"gitops-vn4"}
	for index := 1; index <= 16; index++ {
		ids = append(ids, model.ID(fmt.Sprintf("gitops-vn4.%d", index)))
	}
	f := newFixture(t, ids...)
	parent := f.issue("Sixteen children, seven of them colliding at twelve columns", 1)
	for index := 1; index <= 16; index++ {
		f.child(parent.ID, fmt.Sprintf("Child %d", index), 2)
	}

	issue := viewOf(t, f, parent.ID)
	if pad := idPad(relationGroups(issue)); pad != 13 {
		t.Fatalf("id column = %d, want 13 — the widest id in this picker is gitops-vn4.16", pad)
	}

	rows, choices := followRows(issue)

	// Every title starts in the same column. A capped column pads the short
	// ids and not the long ones, so the two halves of the list disagree about
	// where a title begins — which is what a ragged picker looks like.
	column := -1
	for _, row := range rows {
		at := strings.Index(row.Text, " Child ")
		if column == -1 {
			column = at
		}
		if at != column {
			t.Fatalf("row %q starts its title at column %d, want %d — the id column is ragged",
				row.Text, at, column)
		}
	}

	// And every id still renders IN FULL at a 38-column pane, which is the
	// whole reason the cap is refused: seven of these would otherwise be
	// seven identical rows.
	chooser := newPicker("", rows)
	drawn := nonBlank(chooser.view(38, 20))
	if len(drawn) != 17 {
		t.Fatalf("picker drew %d lines, want 17 (one header, sixteen children)", len(drawn))
	}
	for _, choice := range choices {
		if lineHolding(drawn, string(choice.id)) == "" {
			t.Fatalf("no row renders %s in full:\n%s", choice.id, strings.Join(drawn, "\n"))
		}
	}
}

func TestFOnAnIssueWithNoRelationsOpensNoPicker(t *testing.T) {
	f := newFixture(t)
	f.issue("An issue with nothing to follow", 1)
	pane := f.model(80, 24)

	press(t, pane, "f")
	if pane.modal != nil {
		t.Fatal("f opened a picker over an empty graph; there is nothing to choose from")
	}
	if footer := pane.footer(measure(80, 24, false)); !strings.Contains(footer, "nothing to follow") {
		t.Fatalf("footer = %q, want it to say why nothing happened", footer)
	}
	// A notice lives until the next keypress and no longer.
	press(t, pane, "j")
	if footer := pane.footer(measure(80, 24, false)); strings.Contains(footer, "nothing to follow") {
		t.Fatalf("footer = %q, want the notice gone after the next key", footer)
	}
}

func TestBackspaceAtDepthZeroIsANoOp(t *testing.T) {
	f := newFixture(t)
	only := f.issue("The only issue", 1)
	pane := f.model(80, 24)

	pressNamed(t, pane, tea.KeyBackspace)
	if len(pane.stack) != 0 {
		t.Fatalf("stack = %v, want it still empty", pane.stack)
	}
	if pane.detailID != only.ID {
		t.Fatalf("detail pane shows %s, want %s unmoved", pane.detailID, only.ID)
	}
}

func TestMovingTheLeftCursorClearsTheFollowStack(t *testing.T) {
	// This is what let a key be REMOVED from the design: euv2e.5 gave `esc` a
	// fourth ordered meaning to clear the stack, and `j` — a key you were
	// about to press anyway — already does it.
	f := newFixture(t)
	parent := f.issue("The parent", 1)
	child := f.child(parent.ID, "The child", 2)
	f.issue("Somewhere else in the list", 3)
	pane := f.model(80, 24)

	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.detailID != child.ID || len(pane.stack) != 1 {
		t.Fatalf("after following: detail %s at depth %d, want %s at depth 1",
			pane.detailID, len(pane.stack), child.ID)
	}

	press(t, pane, "j")
	if len(pane.stack) != 0 {
		t.Fatalf("stack after moving the cursor = %v, want it cleared", pane.stack)
	}
	if pane.depthLine(38) != "" {
		t.Fatalf("depth line = %q, want none at depth 0", pane.depthLine(38))
	}
}

func TestFollowingRetargetsTheRightPaneAndBackspaceComesHome(t *testing.T) {
	f := newFixture(t)
	parent := f.issue("The parent", 1)
	child := f.child(parent.ID, "The child", 2)
	grandchild := f.child(child.ID, "The grandchild", 2)
	pane := f.model(80, 24)

	press(t, pane, "f")
	if pane.modal == nil {
		t.Fatal("f opened no picker")
	}
	pressNamed(t, pane, tea.KeyEnter)
	press(t, pane, "f")
	// The child's picker opens on its Parent row, because Parent leads the
	// page's order; `G` is the Children group at the bottom.
	press(t, pane, "G")
	pressNamed(t, pane, tea.KeyEnter)

	if pane.detailID != grandchild.ID {
		t.Fatalf("detail pane shows %s, want %s two follows in", pane.detailID, grandchild.ID)
	}
	if pane.cursor.id != parent.ID || pane.cursor.position != 0 {
		t.Fatalf("cursor moved to %s/%d; following must not move the left cursor",
			pane.cursor.id, pane.cursor.position)
	}
	if want := "← " + string(child.ID) + " ·2"; !strings.Contains(pane.depthLine(38), want) {
		t.Fatalf("depth line = %q, want %q — the origin and the count", pane.depthLine(38), want)
	}

	pressNamed(t, pane, tea.KeyBackspace)
	if pane.detailID != child.ID || len(pane.stack) != 1 {
		t.Fatalf("after backspace: %s at depth %d, want %s at depth 1",
			pane.detailID, len(pane.stack), child.ID)
	}
	pressNamed(t, pane, tea.KeyBackspace)
	if pane.detailID != parent.ID || len(pane.stack) != 0 {
		t.Fatalf("after a second backspace: %s at depth %d, want %s at depth 0",
			pane.detailID, len(pane.stack), parent.ID)
	}
}

func TestFollowingReachesAClosedIssueOutsideTheRowSet(t *testing.T) {
	// 52% of relation targets in the corpus are closed, and every
	// `discovered-from` target is. `C` governs the left pane only.
	f := newFixture(t)
	here := f.issue("The issue the picker is opened on", 1)
	origin := f.issue("Where it came from", 2)
	f.dep(here.ID, origin.ID, model.DepDiscoveredFrom)
	f.setStatus(origin.ID, model.StatusClosed)

	pane := f.model(80, 24)
	if hasID(pane.rows, origin.ID) {
		t.Fatal("the closed target is in the row set; this fixture proves nothing")
	}
	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEnter)
	if pane.detailID != origin.ID {
		t.Fatalf("detail pane shows %s, want the closed target %s", pane.detailID, origin.ID)
	}
}

func TestEscCancelsThePickerAndLeavesTheStackAlone(t *testing.T) {
	f := newFixture(t)
	parent := f.issue("The parent", 1)
	f.child(parent.ID, "The child", 2)
	pane := f.model(80, 24)

	press(t, pane, "f")
	pressNamed(t, pane, tea.KeyEscape)
	if pane.modal != nil {
		t.Fatal("esc did not cancel the picker")
	}
	if pane.detailID != parent.ID {
		t.Fatalf("detail pane shows %s, want %s — cancelling follows nothing", pane.detailID, parent.ID)
	}
}

func TestThePickerCapturesTheKeysTheNavigatorWouldHaveTaken(t *testing.T) {
	// While the picker is open `j` walks IT, not the list, which is what
	// keeps `esc` unambiguous: nothing else is reachable from here.
	f := newFixture(t)
	parent := f.issue("The parent", 1)
	f.child(parent.ID, "First child", 2)
	second := f.child(parent.ID, "Second child", 2)
	pane := f.model(80, 24)

	press(t, pane, "f")
	press(t, pane, "j")
	if pane.modal.selected() != 1 {
		t.Fatalf("picker cursor = %d, want 1 — j walks the picker", pane.modal.selected())
	}
	if pane.cursor.id != parent.ID {
		t.Fatalf("list cursor moved to %s while the picker was open", pane.cursor.id)
	}
	press(t, pane, "g")
	if pane.modal.selected() != 0 {
		t.Fatalf("picker cursor after g = %d, want 0", pane.modal.selected())
	}
	press(t, pane, "G")
	if pane.modal.selected() != 1 {
		t.Fatalf("picker cursor after G = %d, want the last row", pane.modal.selected())
	}
	pressNamed(t, pane, tea.KeyEnter)
	if pane.detailID != second.ID {
		t.Fatalf("enter followed %s, want %s", pane.detailID, second.ID)
	}
}

func TestThePickerOverflowShowsInTheFooterAndNotInTheBox(t *testing.T) {
	// Worst case in the corpus is 23 rows under 4 headers against a 21-row
	// body; a row inside the box saying so would cost the row it reports on.
	ids := []model.ID{"parent"}
	for index := 1; index <= 30; index++ {
		ids = append(ids, model.ID(fmt.Sprintf("parent.%d", index)))
	}
	f := newFixture(t, ids...)
	parent := f.issue("A parent with thirty children", 1)
	for index := 1; index <= 30; index++ {
		f.child(parent.ID, fmt.Sprintf("Child %d", index), 2)
	}
	pane := f.model(80, 24)
	geo := measure(80, 24, false)

	press(t, pane, "f")
	// It counts LINES, not rows: the first child sits on line 2, under the
	// `Children` header, and the header takes room the body has to find.
	if want := "↕ 2 of 31"; !strings.Contains(pane.footer(geo), want) {
		t.Fatalf("footer = %q, want %q — thirty rows under one header", pane.footer(geo), want)
	}
	press(t, pane, "G")
	if want := "↕ 31 of 31"; !strings.Contains(pane.footer(geo), want) {
		t.Fatalf("footer = %q, want %q", pane.footer(geo), want)
	}
	if strings.Contains(pane.frame(), "of 31\n") {
		t.Fatal("the overflow indicator is inside the box; it belongs in the footer")
	}
}

func TestASmallPickerGetsNoOverflowIndicator(t *testing.T) {
	f := newFixture(t)
	parent := f.issue("The parent", 1)
	f.child(parent.ID, "The only child", 2)
	pane := f.model(80, 24)

	press(t, pane, "f")
	if footer := pane.footer(measure(80, 24, false)); strings.Contains(footer, "↕") {
		t.Fatalf("footer = %q, want no indicator when nothing is off-screen", footer)
	}
}

// page renders one view the way the detail pane does, so a test can ask what
// `show` would have printed.
func (f *fixture) page(t *testing.T, issue core.IssueView) string {
	t.Helper()
	source := &source{core: f.core, project: f.project}
	text, err := source.page(issue, 38)
	if err != nil {
		t.Fatalf("render page: %v", err)
	}
	return text
}

func lineHolding(lines []string, needle string) string {
	for _, line := range lines {
		if strings.Contains(line, needle+" ") || strings.HasSuffix(line, needle) {
			return line
		}
	}
	return ""
}

func equalIDs(got, want []model.ID) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
