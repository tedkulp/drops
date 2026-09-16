package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// column is one Board column. It is DERIVED, never stored: CONTEXT.md's
// Ready and Blocked are properties of an open issue rather than statuses, so
// nothing can be put into a column — an issue arrives in one by its status and
// its blockers.
type column int

const (
	colReady column = iota
	colBlocked
	colInProgress
	colClosed
)

// columnCount is how many columns the board has, whatever the width shows.
const columnCount = 4

var columnNames = [columnCount]string{"Ready", "Blocked", "In progress", "Closed"}

// minColumnWidth is the narrowest a column is drawn. What does not fit at it
// is paged sideways rather than squeezed (#2, variant E): four columns in an
// 80-column terminal are slivers nobody can read a title in.
const minColumnWidth = 28

// columnOf derives one issue's column, one branch per clause.
//
// in_progress is asked BEFORE the blocker count, and that order is the rule
// rather than a detail of it: started work that is stuck is still started, so
// a blocked in_progress issue sits in In progress and says `blocked by N` on
// its card instead of looking unstarted.
func columnOf(candidate row) column {
	switch {
	case candidate.status == model.StatusClosed:
		return colClosed
	case candidate.status == model.StatusInProgress:
		return colInProgress
	case candidate.blockedBy > 0:
		return colBlocked
	}
	return colReady
}

// boardCards sets the narrowed rows out in their columns.
//
// Ready, Blocked and In progress come from the list's own row set, in the
// CLI's order, and Closed comes from core's closed read, most recently closed
// first. Neither is re-sorted.
func (m *Model) boardCards() [columnCount][]row {
	var cards [columnCount][]row
	for _, candidate := range append(m.narrow(liveRows(m.all)), m.narrow(m.closed)...) {
		cards[columnOf(candidate)] = append(cards[columnOf(candidate)], candidate)
	}
	return cards
}

// liveRows is the list's rows without the closed ones `C` adds. Those arrive in
// the QUEUE's order, and the board's Closed column is core's order, so the
// board takes its closed cards from the closed read and never from here.
func liveRows(rows []row) []row {
	live := make([]row, 0, len(rows))
	for _, candidate := range rows {
		if candidate.status == model.StatusClosed {
			continue
		}
		live = append(live, candidate)
	}
	return live
}

// shownColumns is how many columns fit a board this wide at minColumnWidth,
// separators included, and never fewer than one.
func shownColumns(boardWidth int) int {
	return max(1, min(columnCount, (boardWidth+1)/(minColumnWidth+1)))
}

// firstColumn is the leftmost column on screen once the focused column has
// been scrolled into view, from whichever side it went off.
func firstColumn(first, focused, shown int) int {
	if focused < first {
		first = focused
	}
	if focused >= first+shown {
		first = focused - shown + 1
	}
	return max(0, min(first, columnCount-shown))
}

// pageBoard keeps the focused column on screen at the current width. It
// measures the board as drawn unzoomed, because `enter`-zoom hides the board
// without changing what it will show when it comes back.
func (m *Model) pageBoard() {
	m.first = firstColumn(m.first, int(m.focus), shownColumns(measureBoard(m.width, m.height).listWidth))
}

// cardLines is how many lines a card takes, and cardsFit how many whole cards
// a column shows under its heading, a blank line between each.
const cardLines = 3

func cardsFit(rows int) int { return max(rows/(cardLines+1), 1) }

// cardTop is the first card a column shows so the selected one is on screen.
// It is derived from the selection rather than held, like the list's window.
func cardTop(selected, fit int) int { return max(0, selected-fit+1) }

// switchColumn is `tab` and `shift+tab`: step one column, wrapping round all
// four, and land on the card at the same row position on screen, or the last
// card above it. The cursor moved, so the follow trail goes with it.
func (m *Model) switchColumn(step int) error {
	fit := cardsFit(m.geo().rows)
	offset := m.cursor.position - cardTop(m.cursor.position, fit)
	m.focus = column((int(m.focus) + step + columnCount) % columnCount)
	m.pageBoard()
	m.clearFollow()
	m.cursor.move(m.current(), offset)
	return m.showDetail(m.cursor.id)
}

// toggleBoard is `B`. It reads nothing: the board's rows are loaded with the
// list's, and everything that narrows or trails — the cursor's id, `/`, `a`,
// `r`, the follow stack — is the model's rather than a view's, so it is
// already there. The right pane is redrawn from the view it already holds at
// the new pane width.
//
// The one read `B` can cause is the list's own rule applying: a closed card
// is not a list row with `C` off, so going back puts the cursor on the row
// that held its place, and a different issue is a page read like any other.
//
// When the cursor does land on a different issue, that is the cursor moving,
// so the follow trail goes with it, as it does for every other cursor move.
func (m *Model) toggleBoard() error {
	carried := m.cursor.id
	m.board = !m.board
	m.resize()
	m.place()
	if m.cursor.id != carried {
		m.clearFollow()
	}
	target := m.cursor.id
	if len(m.stack) > 0 {
		target = m.detailID
	}
	if target != "" && target == m.detailID {
		return m.display(target, m.detailView)
	}
	return m.showDetail(target)
}

// age is how long ago a moment was, in the prototype's buckets: minutes under
// an hour, hours under two days, days under sixty, months under a year, then
// years.
func age(elapsed time.Duration) string {
	const day = 24 * time.Hour
	switch {
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", max(int(elapsed.Minutes()), 0))
	case elapsed < 48*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	case elapsed < 60*day:
		return fmt.Sprintf("%dd", int(elapsed/day))
	case elapsed < 365*day:
		return fmt.Sprintf("%dmo", int(elapsed/(30*day)))
	}
	return fmt.Sprintf("%dy", int(elapsed/(365*day)))
}

// since is the moment a card's age counts from, which depends on the column:
// how long it has waited (Ready, Blocked), been under way (In progress), or
// been finished (Closed). A moment the issue lacks falls back to its creation.
func since(candidate row, col column) *model.Timestamp {
	moment := candidate.created
	switch col {
	case colInProgress:
		moment = candidate.started
	case colClosed:
		moment = candidate.closedAt
	}
	if moment == nil {
		return candidate.created
	}
	return moment
}

// card is one card's three lines, unstyled, none wider than width.
//
//  1. the status mark, priority, type and id, with the age flush right;
//  2. the title;
//  3. whichever of `@assignee` and `blocked by N` are present.
//
// Geometry lives here rather than in render, like the list's row, and the
// primitives are render's.
func card(candidate row, col column, width int, now time.Time) [cardLines]string {
	head := fmt.Sprintf("%s P%d %s %s", render.StatusMark(candidate.status, candidate.tombstoned),
		candidate.priority, candidate.kind, candidate.id)
	when := ""
	if moment := since(candidate, col); moment != nil {
		if at, err := moment.Time(); err == nil {
			when = age(now.Sub(at))
		}
	}

	var facts []string
	if candidate.assignee != "" {
		facts = append(facts, "@"+candidate.assignee)
	}
	if candidate.blockedBy > 0 {
		facts = append(facts, fmt.Sprintf("blocked by %d", candidate.blockedBy))
	}
	return [cardLines]string{
		flush(head, when, width),
		render.Ellipsis(candidate.title, width),
		render.Ellipsis(strings.Join(facts, " · "), width),
	}
}

// flush puts right against the right edge and cuts left to what is left.
func flush(left, right string, width int) string {
	if right == "" {
		return render.Ellipsis(left, width)
	}
	room := width - render.Cols(right) - 1
	if room < 1 {
		return render.Ellipsis(right, width)
	}
	left = render.Ellipsis(left, room)
	return left + strings.Repeat(" ", room-render.Cols(left)) + " " + right
}

// heading is a column's name and count. `▸ ` marks the focused column and
// only that one; the others get two spaces, so names line up either way.
func heading(col column, count int, focused bool) string {
	marker := "  "
	if focused {
		marker = "▸ "
	}
	return fmt.Sprintf("%s%s %d", marker, columnNames[col], count)
}

// columnBody renders one column into exactly width × height. selected is the
// cursor's card, or -1 in a column that is not focused.
func columnBody(col column, cards []row, selected, width, height int, now time.Time) string {
	focused := selected >= 0
	title := render.Ellipsis(heading(col, len(cards), focused), width)
	if focused {
		title = headStyle.Render(title)
	} else {
		title = dimStyle.Render(title)
	}
	lines := []string{title}

	fit := cardsFit(height)
	top := cardTop(max(selected, 0), fit)
	for index := top; index < len(cards) && index < top+fit; index++ {
		if index > top {
			lines = append(lines, "")
		}
		for _, line := range card(cards[index], col, width, now) {
			// Padded before styling, so reverse video covers the card's
			// whole width rather than stopping at its text.
			line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
			switch {
			case focused && index == selected:
				line = cursorStyle.Render(line)
			case cards[index].status == model.StatusClosed:
				line = dimStyle.Render(line)
			}
			lines = append(lines, line)
		}
	}
	return pad(strings.Join(lines, "\n"), width, height)
}

// boardBody is the columns on screen, side by side with a dim `│` between,
// exactly width × height. The last column takes what integer division left.
//
// The columns start at m.first, which pageBoard keeps current on every focus
// change and resize, so drawing reads it rather than moving it.
func (m *Model) boardBody(width, height int) string {
	shown := shownColumns(width)
	columnWidth := max((width-(shown-1))/shown, 1)
	now := m.now()

	blocks := make([][]string, 0, shown)
	for index := m.first; index < m.first+shown; index++ {
		col := column(index)
		span := columnWidth
		if index == m.first+shown-1 {
			span = max(width-(shown-1)-(shown-1)*columnWidth, 1)
		}
		selected := -1
		if col == m.focus {
			selected = m.cursor.position
		}
		blocks = append(blocks, strings.Split(columnBody(col, m.cards[col], selected, span, height, now), "\n"))
	}

	separator := dimStyle.Render("│")
	lines := make([]string, 0, height)
	for line := range height {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			parts = append(parts, block[line])
		}
		lines = append(lines, strings.Join(parts, separator))
	}
	return pad(strings.Join(lines, "\n"), width, height)
}

// boardTitle names the scope, and says how many columns are off screen on
// each side while any are.
func (m *Model) boardTitle(width int) string {
	title := m.listTitle()
	shown := shownColumns(width)
	if shown < columnCount {
		title += fmt.Sprintf(" · ◀ %d · %d ▶", m.first, columnCount-m.first-shown)
	}
	return title
}

// boardCount is how many cards the board shows.
func (m *Model) boardCount() int {
	total := 0
	for _, cards := range m.cards {
		total += len(cards)
	}
	return total
}

// boardLoaded is how many cards the board would show with nothing narrowing:
// the live rows and core's closed rows, each once.
func (m *Model) boardLoaded() int { return len(liveRows(m.all)) + len(m.closed) }
