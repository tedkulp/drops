package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// idCap and projCap are qy3de.4's: auto-size the column to the widest value
// present, but never past twelve, and truncate what does not fit.
//
// This deliberately breaks render's "truncate what it DISPLAYS, never what
// IDENTIFIES" rule, and it is allowed to because the pane puts the full id
// back on the detail pane's border one keypress away. Measured: 44 of 46 open
// ids in `drops` are eleven columns or fewer and exactly two are 24 and 31, so
// the CLI's pad-to-widest spends twenty dead columns on 44 rows to fit two.
const (
	idCap   = 12
	projCap = 12
)

// row is one left-pane row's data, before any geometry.
type row struct {
	id         model.ID
	project    string
	status     model.Status
	priority   int
	title      string
	tombstoned bool
	// blockedBy is how many OPEN blockers this issue still has. It comes
	// from one core.OpenBlockers call per refresh, never one read per row.
	blockedBy int
}

// scope is what the row set covers: qy3de.4's `a` and `C`.
type scope struct {
	allProjects   bool
	includeClosed bool
}

// statuses is the row set's status filter. `list`'s contract, not `ready`'s:
// open AND in_progress, so the issue you are actively working on is in the
// pane you navigate from while working. `ready` structurally omits it.
func (s scope) statuses() []model.Status {
	live := []model.Status{model.StatusOpen, model.StatusInProgress}
	if s.includeClosed {
		return append(live, model.StatusClosed)
	}
	return live
}

// matches is qy3de.4's `/`: a case-insensitive literal substring over the id
// and the title. Literal rather than fuzzy because ids are structured, and
// fuzzy matching over dotted ids is noise. needle arrives lowercased.
func matches(candidate row, needle string) bool {
	if needle == "" {
		return true
	}
	return strings.Contains(strings.ToLower(string(candidate.id)), needle) ||
		strings.Contains(strings.ToLower(candidate.title), needle)
}

// matching keeps the rows matching needle, in order. It never re-sorts: the
// order is the CLI's, achieved by leaving the store's alone.
func matching(rows []row, needle string) []row {
	needle = strings.ToLower(needle)
	kept := make([]row, 0, len(rows))
	for _, candidate := range rows {
		if matches(candidate, needle) {
			kept = append(kept, candidate)
		}
	}
	return kept
}

// ready keeps the rows with no open blocker, in order. It never re-sorts,
// for the same reason matching does not: the order is the CLI's, achieved by
// leaving the store's alone.
//
// It is deliberately NOT core.Ready, and g7b23 exists because that is the
// trap. core.Ready sets filter.Statuses = {open} unconditionally and so cannot
// see an in_progress issue at all (drops://8bbam), which is precisely the row
// set qy3de.4 rejected for this pane: it would structurally hide the issue you
// are working on. This is a local predicate over rows the pane already has —
// blockedBy, annotated once per refresh from the store-wide
// core.OpenBlockers — so the row set stays `list`'s contract, no key can reach
// 8bbam, and it composes with `/`, `C` and `a` the way matching already does.
func ready(rows []row) []row {
	kept := make([]row, 0, len(rows))
	for _, candidate := range rows {
		if candidate.blockedBy == 0 {
			kept = append(kept, candidate)
		}
	}
	return kept
}

// cursor is the left pane's selection. It tracks an issue ID, not a row index,
// so a reload that reorders or removes rows cannot move the selection under
// the reader. position is the fallback, not the identity.
type cursor struct {
	id       model.ID
	position int
}

// restore re-finds the tracked id in a new row set. When that id is still
// present the cursor follows it wherever it moved; when it has left — filtered
// out, closed with `C` off, deleted — the cursor HOLDS the position index that
// row last occupied, clamped to the new bounds, so the reader stays where they
// were looking instead of being thrown to the top.
func (c *cursor) restore(rows []row) {
	for index, candidate := range rows {
		if candidate.id == c.id {
			c.position = index
			return
		}
	}
	if c.position >= len(rows) {
		c.position = len(rows) - 1
	}
	if c.position < 0 {
		c.position = 0
	}
	if len(rows) == 0 {
		c.id = ""
		return
	}
	c.id = rows[c.position].id
}

// move puts the cursor on one row by index, clamped.
func (c *cursor) move(rows []row, to int) {
	if len(rows) == 0 {
		c.id, c.position = "", 0
		return
	}
	c.position = min(max(to, 0), len(rows)-1)
	c.id = rows[c.position].id
}

// rowLayout is the left pane's column geometry, measured across the whole row
// set exactly once — the way render.measure does it for the CLI.
type rowLayout struct {
	idPad   int
	projPad int // zero when the project column is off
	width   int
}

// measureRows sizes the columns to the widest value present, capped.
func measureRows(rows []row, showProject bool, width int) rowLayout {
	measured := rowLayout{width: width}
	for _, candidate := range rows {
		measured.idPad = max(measured.idPad, render.Cols(string(candidate.id)))
		if showProject {
			measured.projPad = max(measured.projPad, render.Cols(candidate.project))
		}
	}
	measured.idPad = min(measured.idPad, idCap)
	measured.projPad = min(measured.projPad, projCap)
	return measured
}

// lead is everything left of the title: the CLI's row minus its type column.
// Eight columns to say `task` on a pane whose whole job is navigation, with
// the detail pane one keypress away, is the cheapest eight columns to spend.
//
// The glyphs stay render's, so a listing and a page can never disagree about
// what a state looks like.
func (measured rowLayout) lead(candidate row) string {
	lead := fmt.Sprintf("%s %-*s P%d",
		render.StatusMark(candidate.status, candidate.tombstoned),
		measured.idPad, render.Ellipsis(string(candidate.id), measured.idPad),
		candidate.priority)
	if measured.projPad > 0 {
		lead += fmt.Sprintf(" %-*s", measured.projPad, render.Ellipsis(candidate.project, measured.projPad))
	}
	return lead
}

// marker is how a blocked row says so: four columns, not thirteen.
//
// qy3de.5 measured the alternatives on the 13 blocked rows of the corpus. At
// 80 columns with the project column on, the pane is 38 wide and the lead is
// 31, so a 13-column " blocked by N" leaves NEGATIVE room and all 13 rows
// render with no title at all. A rule that drops the long form under pressure
// was rejected for a different reason: qy3de.4 denied a ready-only key BECAUSE
// the row already carries this, and a marker that disappears when the pane is
// crowded cannot carry that argument. g7b23 has since given that key out on a
// real-use observation, which does not weaken the argument here: the marker is
// what tells you the rows `r` would hide are there at all.
func marker(candidate row) string {
	if candidate.blockedBy == 0 {
		return ""
	}
	return fmt.Sprintf(" [%d]", candidate.blockedBy)
}

// line composes one row.
//
// The marker is appended AFTER the title is cut, never reserved before it —
// euv2e.4 recorded that trap and it is paid for here. The cut itself goes
// through render.Ellipsis rather than a second implementation of the same
// rule.
func (measured rowLayout) line(candidate row) string {
	lead := measured.lead(candidate)
	mark := marker(candidate)
	title := render.Ellipsis(candidate.title, measured.width-render.Cols(lead)-1-render.Cols(mark))
	if title == "" {
		return lead + mark
	}
	return lead + " " + title + mark
}

// listBody renders the visible window of rows into exactly height lines,
// scrolled so the cursor is on screen.
func listBody(rows []row, selected, width, height int, showProject bool, empty string) string {
	if len(rows) == 0 {
		return pad(dimStyle.Render(render.Ellipsis(empty, width)), width, height)
	}
	measured := measureRows(rows, showProject, width)

	top := 0
	if selected >= height {
		top = selected - height + 1
	}
	top = max(0, min(top, len(rows)-height))

	lines := make([]string, 0, height)
	for index := top; index < len(rows) && len(lines) < height; index++ {
		line := measured.line(rows[index])
		// Padded to the full width before styling, so the cursor's reverse
		// video covers the whole row rather than stopping at the title.
		line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
		if index == selected {
			line = cursorStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return pad(strings.Join(lines, "\n"), width, height)
}
