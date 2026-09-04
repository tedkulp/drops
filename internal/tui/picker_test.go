package tui

import (
	"strings"
	"testing"
)

// pickerOf builds a picker from `group|text` pairs, so a layout test reads as
// the rows it is about and nothing else.
func pickerOf(title string, pairs ...string) picker {
	rows := make([]pickerRow, 0, len(pairs))
	for _, pair := range pairs {
		group, text, _ := strings.Cut(pair, "|")
		rows = append(rows, pickerRow{Group: group, Text: text})
	}
	return newPicker(title, rows)
}

func TestPickerMoveClampsAtBothEnds(t *testing.T) {
	chooser := pickerOf("", "Children|one", "Children|two", "Children|three")

	chooser.move(1)
	if chooser.selected() != 1 {
		t.Fatalf("cursor after one step = %d, want 1", chooser.selected())
	}
	// `G` is move with a delta longer than the list, so the clamp IS the key.
	chooser.move(100)
	if chooser.selected() != 2 {
		t.Fatalf("cursor after G = %d, want the last row (2)", chooser.selected())
	}
	chooser.move(-100)
	if chooser.selected() != 0 {
		t.Fatalf("cursor after g = %d, want the first row (0)", chooser.selected())
	}
}

func TestPickerMoveOverNoRowsStaysPut(t *testing.T) {
	chooser := pickerOf("Priority")
	chooser.move(1)
	if chooser.selected() != 0 {
		t.Fatalf("cursor over an empty picker = %d, want 0", chooser.selected())
	}
}

func TestPickerEmitsAGroupHeaderExactlyOnAChange(t *testing.T) {
	// Not per row, and not once: a header is one line per group, and the
	// cursor cannot land on one because headers are never stored as rows.
	chooser := pickerOf("",
		"Blocked by|○ a  first blocker",
		"Blocked by|● b  second blocker",
		"Children|○ c  a child")

	drawn := chooser.view(40, 10)
	if got := strings.Count(drawn, "Blocked by"); got != 1 {
		t.Fatalf("`Blocked by` appears %d times, want exactly one header:\n%s", got, drawn)
	}
	if got := strings.Count(drawn, "Children"); got != 1 {
		t.Fatalf("`Children` appears %d times, want exactly one header:\n%s", got, drawn)
	}
	// Three rows under two headers is five lines, and nothing else.
	if got := len(nonBlank(drawn)); got != 5 {
		t.Fatalf("picker drew %d lines, want 5 (two headers, three rows):\n%s", got, drawn)
	}
}

func TestPickerDrawsNoHeaderForAnUngroupedRow(t *testing.T) {
	// qy3de.7's `Priority` picker is ungrouped and carries a title instead;
	// that is what the title parameter exists for, and the follow picker
	// passes none because its group headers already say what the rows are.
	chooser := pickerOf("Priority", "|0", "|1", "|2")
	drawn := nonBlank(chooser.view(30, 10))
	if len(drawn) != 4 {
		t.Fatalf("picker drew %d lines, want 4 (a title and three rows):\n%v", len(drawn), drawn)
	}
	if !strings.Contains(drawn[0], "Priority") {
		t.Fatalf("first line = %q, want the title", drawn[0])
	}
}

func TestPickerScrollCountsHeaderLinesAndNotRows(t *testing.T) {
	// The one place row indices and line indices disagree. Four rows under
	// two headers is SIX lines: the last row sits on line 5, not line 3, so
	// a window computed from the row index leaves it off a four-line body.
	chooser := pickerOf("", "A|a1", "A|a2", "B|b1", "B|b2")

	chooser.move(3)
	chooser.scroll(4)
	if chooser.offset != 2 {
		t.Fatalf("offset = %d, want 2 — the cursor is on rendered line 5, not line 3", chooser.offset)
	}
	if got := chooser.lineOf(chooser.selected()); got != 5 {
		t.Fatalf("cursor's rendered line = %d, want 5", got)
	}
}

func TestPickerViewShowsTheCursorRowWhenHeadersPushItDown(t *testing.T) {
	// view re-derives the window rather than trusting offset, so a picker
	// drawn without a scroll — after a resize, or in a test — still shows the
	// row under the cursor.
	chooser := pickerOf("", "A|a1", "A|a2", "B|b1", "B|b2")
	chooser.move(3)

	drawn := chooser.view(20, 4)
	if !strings.Contains(drawn, "b2") {
		t.Fatalf("the cursor's row is off-screen; the window counted rows, not lines:\n%s", drawn)
	}
	if strings.Contains(drawn, "a1") {
		t.Fatalf("window = %q, want it scrolled past the first group", drawn)
	}
}

func TestPickerScrollsNoFurtherThanItHasTo(t *testing.T) {
	// Minimal scroll, not the left pane's cursor-to-the-bottom rule: a picker
	// is walked in both directions, and a window that jumps every time you
	// move back up loses the group you were reading.
	chooser := pickerOf("", "A|a1", "A|a2", "A|a3", "A|a4", "A|a5")
	chooser.move(4)
	chooser.scroll(3)
	if chooser.offset != 3 {
		t.Fatalf("offset at the bottom = %d, want 3", chooser.offset)
	}
	chooser.move(-1)
	chooser.scroll(3)
	if chooser.offset != 3 {
		t.Fatalf("offset after stepping back onto a visible row = %d, want it unmoved at 3", chooser.offset)
	}
}

func TestPickerLineCountIsWhatViewDraws(t *testing.T) {
	// The footer's overflow indicator reads lineCount and the body draws
	// view; they have to be the same number or the indicator lies.
	for _, chooser := range []picker{
		pickerOf("", "A|a1", "A|a2", "B|b1"),
		pickerOf("Priority", "|0", "|1"),
		pickerOf(""),
	} {
		drawn := chooser.view(20, 40)
		if got, want := len(nonBlank(drawn)), chooser.lineCount(); got != want {
			t.Fatalf("view drew %d lines, lineCount says %d:\n%s", got, want, drawn)
		}
	}
}

func TestPickerSquaresItsBodyToTheGeometryItIsGiven(t *testing.T) {
	chooser := pickerOf("", "A|a very long row that will not fit inside a narrow pane at all", "A|b")
	assertSquare(t, "picker", chooser.view(24, 6), 24, 6)
}

// nonBlank drops the padding view adds to square its body off.
func nonBlank(block string) []string {
	kept := []string{}
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return kept
}
