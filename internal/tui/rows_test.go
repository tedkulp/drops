package tui

import (
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// rowsOf is the row set a layout test works over, written as literals so the
// assertion is about geometry and nothing else.
func rowsOf(ids ...model.ID) []row {
	out := make([]row, 0, len(ids))
	for _, id := range ids {
		out = append(out, row{id: id, status: model.StatusOpen, priority: 2, title: string(id) + " title"})
	}
	return out
}

func TestCursorTracksAnIDAcrossAReorder(t *testing.T) {
	before := rowsOf("alpha", "bravo", "charlie")
	var selection cursor
	selection.move(before, 1)
	if selection.id != "bravo" {
		t.Fatalf("cursor id = %q, want bravo", selection.id)
	}

	// The same rows, reordered and one row longer above the tracked id: a
	// poll or a reload does exactly this.
	after := rowsOf("delta", "echo", "charlie", "bravo", "alpha")
	selection.restore(after)

	if selection.id != "bravo" {
		t.Fatalf("cursor id after reorder = %q, want bravo — the cursor tracks an id, not an index", selection.id)
	}
	if selection.position != 3 {
		t.Fatalf("cursor position after reorder = %d, want 3 (where bravo now is)", selection.position)
	}
}

func TestCursorHoldsItsPositionWhenTheTrackedIDLeaves(t *testing.T) {
	rows := rowsOf("alpha", "bravo", "charlie", "delta")
	var selection cursor
	selection.move(rows, 2)

	// charlie leaves the set — filtered out, closed with `C` off, deleted.
	selection.restore(rowsOf("alpha", "bravo", "delta"))

	if selection.position != 2 {
		t.Fatalf("position after the tracked id left = %d, want 2 held", selection.position)
	}
	if selection.id != "delta" {
		t.Fatalf("cursor id after the tracked id left = %q, want delta (index 2 of the new set)", selection.id)
	}
}

func TestCursorClampsWhenTheHeldPositionIsPastTheEnd(t *testing.T) {
	rows := rowsOf("alpha", "bravo", "charlie")
	var selection cursor
	selection.move(rows, 2)

	selection.restore(rowsOf("alpha"))
	if selection.position != 0 || selection.id != "alpha" {
		t.Fatalf("cursor = %d/%q, want 0/alpha", selection.position, selection.id)
	}

	selection.restore(nil)
	if selection.position != 0 || selection.id != "" {
		t.Fatalf("cursor over an empty set = %d/%q, want 0 and no id", selection.position, selection.id)
	}
}

func TestFilterMatchesRegardlessOfCase(t *testing.T) {
	rows := []row{{id: "alpha", title: "Cutover The Store"}, {id: "bravo", title: "Something else"}}

	kept := matching(rows, "cutover")
	if len(kept) != 1 || kept[0].id != "alpha" {
		t.Fatalf("filter %q kept %v, want just alpha — the match is case-insensitive", "cutover", ids(kept))
	}
	if kept := matching(rows, "CUTOVER"); len(kept) != 1 || kept[0].id != "alpha" {
		t.Fatalf("filter %q kept %v, want just alpha", "CUTOVER", ids(kept))
	}
}

func TestFilterMatchesAnIDAsWellAsATitle(t *testing.T) {
	// qy3de.4: a literal substring over the id AND the title. The id here
	// appears in no title, so a title-only predicate keeps nothing.
	rows := []row{{id: "qy3de.11", title: "Build the frame"}, {id: "k3f9x", title: "Something else"}}

	kept := matching(rows, "qy3de")
	if len(kept) != 1 || kept[0].id != "qy3de.11" {
		t.Fatalf("filter %q kept %v, want just qy3de.11 — the filter reaches the id", "qy3de", ids(kept))
	}
	if kept := matching(rows, "frame"); len(kept) != 1 || kept[0].id != "qy3de.11" {
		t.Fatalf("filter %q kept %v, want just qy3de.11 — the filter reaches the title", "frame", ids(kept))
	}
}

func TestFilterIsLiteralAndNotFuzzy(t *testing.T) {
	// Ids are structured, so fuzzy matching over dotted ids is noise: the
	// characters of "qde" appear in order inside "qy3de" and must not match.
	if kept := matching([]row{{id: "qy3de.11", title: "Build the frame"}}, "qde"); len(kept) != 0 {
		t.Fatalf("filter %q kept %v, want nothing — the match is a literal substring", "qde", ids(kept))
	}
}

func TestRowAppendsTheBlockedMarkerAfterTheCut(t *testing.T) {
	const width = 38
	blocked := row{id: "euv2e.10", status: model.StatusOpen, priority: 2, blockedBy: 1,
		title: "The action modal and the four writes"}
	measured := measureRows([]row{blocked}, false, width)

	line := measured.line(blocked)
	if render.Cols(line) != width {
		t.Fatalf("blocked row = %q (%d columns), want exactly %d — the marker is appended AFTER the cut, "+
			"so the cut has to pay for it", line, render.Cols(line), width)
	}
	if !strings.HasSuffix(line, " [1]") {
		t.Fatalf("blocked row = %q, want it to end in the compact marker", line)
	}
	if !strings.Contains(line, "…") {
		t.Fatalf("blocked row = %q, want the title cut with an ellipsis", line)
	}
}

func TestAnUnblockedRowCarriesNoMarker(t *testing.T) {
	open := row{id: "alpha", status: model.StatusOpen, priority: 2, title: "Short"}
	line := measureRows([]row{open}, false, 38).line(open)
	if strings.Contains(line, "[") {
		t.Fatalf("unblocked row = %q, want no marker at all", line)
	}
	if line != "○ alpha P2 Short" {
		t.Fatalf("row = %q, want %q", line, "○ alpha P2 Short")
	}
}

func TestIDColumnAutoSizesButCapsAtTwelve(t *testing.T) {
	// 44 of 46 open ids in `drops` fit in eleven columns and two are 24 and
	// 31, so pad-to-widest would spend twenty dead columns on 44 rows.
	long := row{id: "beacon-responsive-layout-pass-6w0g", status: model.StatusOpen, priority: 2, title: "Long id"}
	short := row{id: "alpha", status: model.StatusOpen, priority: 2, title: "Short id"}
	measured := measureRows([]row{long, short}, false, 60)

	if measured.idPad != idCap {
		t.Fatalf("id column = %d columns, want it capped at %d", measured.idPad, idCap)
	}
	line := measured.line(long)
	if !strings.HasPrefix(line, "○ beacon-resp… P2 ") {
		t.Fatalf("row = %q, want the id cut to twelve columns with an ellipsis", line)
	}
	if render.Cols(measured.line(short)) > 60 {
		t.Fatalf("short row = %q, want it inside the pane", measured.line(short))
	}
}

func TestIDColumnStaysNarrowerThanTheCapWhenItCan(t *testing.T) {
	measured := measureRows(rowsOf("alpha", "bravo"), false, 60)
	if measured.idPad != 5 {
		t.Fatalf("id column = %d, want 5 — it auto-sizes to the widest id present", measured.idPad)
	}
}

func TestProjectColumnAppearsOnlyWhenAskedForAndCapsAtTwelve(t *testing.T) {
	wide := row{id: "alpha", project: "persistentpin-webextension",
		status: model.StatusOpen, priority: 2, title: "Wide project"}

	off := measureRows([]row{wide}, false, 60)
	if off.projPad != 0 {
		t.Fatalf("project column = %d columns with the project column off, want 0", off.projPad)
	}
	if strings.Contains(off.line(wide), "persistentpin") {
		t.Fatalf("row = %q, want no project column", off.line(wide))
	}

	on := measureRows([]row{wide}, true, 60)
	if on.projPad != projCap {
		t.Fatalf("project column = %d columns, want it capped at %d", on.projPad, projCap)
	}
	if !strings.Contains(on.line(wide), "persistentp… ") {
		t.Fatalf("row = %q, want the project slug cut to twelve columns", on.line(wide))
	}
}

func TestRowGlyphsAreRendersOwn(t *testing.T) {
	// A listing and a page can never disagree about what a state looks like,
	// which is the whole reason the type column could be dropped.
	for _, testcase := range []struct {
		status model.Status
		want   string
	}{
		{model.StatusOpen, "○"},
		{model.StatusInProgress, "◐"},
		{model.StatusClosed, "●"},
	} {
		candidate := row{id: "alpha", status: testcase.status, priority: 1, title: "x"}
		line := measureRows([]row{candidate}, false, 30).line(candidate)
		if !strings.HasPrefix(line, testcase.want+" ") {
			t.Fatalf("%s row = %q, want it to open with %q", testcase.status, line, testcase.want)
		}
	}
}

func ids(rows []row) []model.ID {
	out := make([]model.ID, 0, len(rows))
	for _, candidate := range rows {
		out = append(out, candidate.id)
	}
	return out
}

func TestReadyKeepsOnlyTheRowsWithNoOpenBlocker(t *testing.T) {
	// Deliberately NOT core.Ready's row set: this is a predicate over the
	// rows the pane already holds, so the only thing it may drop is a row
	// with an open blocker. The in_progress row here has to survive it —
	// the pane lists `list`'s contract and `r` does not narrow status.
	rows := []row{
		{id: "alpha", status: model.StatusOpen, blockedBy: 0},
		{id: "bravo", status: model.StatusOpen, blockedBy: 2},
		{id: "charlie", status: model.StatusInProgress, blockedBy: 0},
	}

	kept := ready(rows)
	if got := ids(kept); len(got) != 2 || got[0] != "alpha" || got[1] != "charlie" {
		t.Fatalf("ready kept %v, want alpha and charlie — the predicate is blockedBy == 0", got)
	}
}
