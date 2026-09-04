package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/tedkulp/drops/internal/render"
)

// pickerRow is one selectable line and the group it sits under.
//
// Group is a LABEL, not a structure: view emits a header wherever it changes,
// so the caller writes its rows in group order and nothing here re-sorts them.
// A row whose Group is empty gets no header at all, which is what an ungrouped
// picker is (qy3de.7's `Priority` → `0 1 2 3 4`).
//
// Text is composed by the caller and arrives UNTRUNCATED: view is the only
// place that knows the width, so a resize re-cuts the same text rather than
// leaving a row cut for a pane that is no longer there.
type pickerRow struct{ Group, Text string }

// picker is the modal that replaces the detail pane's body (qy3de.6 §3).
//
// That there is a modal at all is forced, not chosen: qy3de.3 made the detail
// pane an opaque render.Console buffer, so the ref lines already on screen are
// not addressable, and re-listing them from the core.IssueView is the only way
// to interact with one without breaking that opacity. The cost — the picker
// re-shows what the pane is already displaying — is the price of that seam.
//
// Two properties carry the design:
//
// Headers are emitted by view on a group change and are NEVER stored as rows,
// so the cursor cannot land on one and the skip-the-header branch never has to
// exist. That is a branch removed rather than tested.
//
// selected returns an INDEX, not a value. The caller builds its own choices
// and these rows in one pass, so the two are aligned by construction; a
// `Value any` on the row would convert an index bug into a type-assertion
// panic, and a generic picker would force the model to hold one field per
// instantiation when only one modal is ever open.
type picker struct {
	title string
	rows  []pickerRow
	// cursor indexes rows. offset is the first visible RENDERED line, which
	// is a different coordinate: headers sit between rows and shift every
	// line below them (qy3de.6 §10 control 3).
	cursor int
	offset int
}

func newPicker(title string, rows []pickerRow) picker {
	return picker{title: title, rows: rows}
}

// move walks the cursor, clamped at both ends. `g` and `G` are this with a
// delta longer than the list rather than two more branches to get wrong.
func (p *picker) move(delta int) {
	if len(p.rows) == 0 {
		p.cursor = 0
		return
	}
	p.cursor = min(max(p.cursor+delta, 0), len(p.rows)-1)
}

// selected is the index of the row under the cursor.
func (p picker) selected() int { return p.cursor }

// headerAt reports whether a group header is emitted above this row: on the
// first row of each group, and nowhere else.
func (p picker) headerAt(index int) bool {
	if p.rows[index].Group == "" {
		return false
	}
	return index == 0 || p.rows[index].Group != p.rows[index-1].Group
}

// titleLines is the heading's height: one line, or none when the caller passed
// no title. The follow picker passes none — its group headers already say what
// the rows are, and the box's border title stays the frame's untruncated id,
// because you are picking a relation *of* that issue.
func (p picker) titleLines() int {
	if p.title == "" {
		return 0
	}
	return 1
}

// lineOf is the rendered line a row lands on: the heading, every header
// emitted above it, and the rows themselves. It is the one place row indices
// and line indices are converted, and getting it wrong is invisible until a
// header pushes the cursor off the bottom of the window.
func (p picker) lineOf(index int) int {
	line := p.titleLines()
	for above := 0; above <= index && above < len(p.rows); above++ {
		if p.headerAt(above) {
			line++
		}
	}
	return line + index
}

// lineCount is how many lines view will emit. It is derived from the same two
// rules lineOf is, so the footer's overflow indicator and the scroll window
// cannot disagree with what is drawn.
func (p picker) lineCount() int {
	if len(p.rows) == 0 {
		return p.titleLines()
	}
	return p.lineOf(len(p.rows)-1) + 1
}

// scroll remembers the window a body of this height should show. It is called
// when the cursor moves, because move deliberately does not know the geometry.
func (p *picker) scroll(height int) {
	p.offset = window(p.offset, p.lineOf(p.cursor), p.lineCount(), height)
}

// window is the MINIMAL scroll that keeps one line inside a height-line body.
//
// Minimal rather than the left pane's cursor-to-the-bottom rule: a picker is
// walked in both directions over a handful of rows, and a window that jumps
// every time you move back up loses the group you were reading.
func window(offset, line, total, height int) int {
	if height < 1 {
		return 0
	}
	if line < offset {
		offset = line
	}
	if line >= offset+height {
		offset = line - height + 1
	}
	return max(min(offset, total-height), 0)
}

// view draws the picker into exactly width × height.
//
// It re-derives the window from the cursor rather than trusting offset, so a
// picker rendered without a scroll — the frame after a resize, a test — still
// shows the row under the cursor.
func (p picker) view(width, height int) string {
	lines := make([]string, 0, p.lineCount())
	if p.title != "" {
		lines = append(lines, headStyle.Render(render.Ellipsis(p.title, width)))
	}
	for index, row := range p.rows {
		if p.headerAt(index) {
			lines = append(lines, dimStyle.Render(render.Ellipsis(row.Group, width)))
		}
		text := render.Ellipsis(row.Text, width)
		if index == p.cursor {
			// Padded to the full width before styling, so the cursor's
			// reverse video covers the whole row rather than stopping at
			// the title — the same rule the left pane's rows follow.
			text += strings.Repeat(" ", max(0, width-lipgloss.Width(text)))
			text = cursorStyle.Render(text)
		}
		lines = append(lines, text)
	}
	top := window(p.offset, p.lineOf(p.cursor), len(lines), height)
	return pad(strings.Join(lines[top:min(top+height, len(lines))], "\n"), width, height)
}
