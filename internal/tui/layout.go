package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// split is how much of the frame the left pane gets. Half: qy3de.5 measured
// the alternatives and nothing argued against an even split — moving it to
// 0.65 buys 18 columns of title and leaves the detail pane at 26.
const split = 0.5

// footerHeight is the one line under the panes. minBodyHeight keeps the frame
// composable on a terminal too short to hold anything.
const (
	footerHeight  = 1
	minBodyHeight = 3
)

// geometry is the whole frame's arithmetic, done once per render.
//
// Every width here is a CONTENT width. The box that holds one is two columns
// wider, because lipgloss v2's Style.Width is the TOTAL rendered width with
// borders included (qy3de.2) — get that backwards and the top edge and the
// body differ by exactly two columns.
type geometry struct {
	listWidth   int // zero when the frame is zoomed to the detail pane
	detailWidth int
	bodyHeight  int
	rows        int // content rows in each pane
}

// measure lays out one frame. zoomed is `enter`: the two columns are dropped
// for the issue text alone.
func measure(width, height int, zoomed bool) geometry {
	measured := geometry{bodyHeight: max(height-footerHeight, minBodyHeight)}
	// A box costs its top and bottom edge. Both panes get the same budget,
	// once, here — leaving each pane to work out its own height is what left
	// euv2e.4's two panes at different heights, since lipgloss Height() is
	// the OUTER height.
	measured.rows = max(measured.bodyHeight-2, 1)

	if zoomed {
		measured.detailWidth = max(width-2, 1)
		return measured
	}
	outerLeft := int(float64(width) * split)
	measured.listWidth = max(outerLeft-2, 1)
	measured.detailWidth = max(width-outerLeft-2, 1)
	return measured
}

// pad squares a block off to exactly width × height, so the two panes are the
// same height and every finished frame line measures the same. Heights are
// managed here and never by a Style, for the reason measure gives.
func pad(block string, width, height int) string {
	lines := strings.Split(block, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	lines = lines[:height]
	for index, line := range lines {
		if lipgloss.Width(line) > width {
			line = cut(line, width)
		}
		lines[index] = line + strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	}
	return strings.Join(lines, "\n")
}

// cut drops runes off the end of a line until it fits width. It measures with
// lipgloss.Width because a line reaching here may carry SGR escapes, but it
// cannot preserve an escape sequence or trailing reset that it cuts through.
func cut(line string, width int) string {
	runes := []rune(line)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

// box draws a rounded border around width content columns, with title on the
// top edge. That title is where the detail pane's FULL untruncated id lives,
// which is what makes the left pane's 12-column id cap honest: the id is
// recoverable one glance away (qy3de.5).
func box(inner string, width int, title string) string {
	body := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder(), false, true, true, true).
		BorderForeground(borderColor).
		// Total rendered width, borders included, so this is width+2.
		Width(width + 2).
		Render(inner)
	return borderTop(width+2, title) + "\n" + body
}

// borderTop is the hand-rolled border label from qy3de.2: lipgloss v2 has no
// border-title API, and splicing a title into an already-styled line
// overwrites the escape sequence rather than the corner rune. So the edge is
// built from the Border's own runes, measured with lipgloss.Width, and
// coloured once at the end.
func borderTop(outer int, title string) string {
	border := lipgloss.RoundedBorder()
	label := ""
	if title != "" {
		label = " " + title + " "
	}
	// One column of Top before the label, then the rest after it.
	fill := outer - 2 - 1 - lipgloss.Width(label)
	if fill < 0 {
		// A title too long for its own edge is dropped rather than
		// widening the frame past the pane it names.
		label, fill = "", outer-3
	}
	return lipgloss.NewStyle().Foreground(borderColor).Render(
		border.TopLeft + border.Top + label + strings.Repeat(border.Top, max(fill, 0)) + border.TopRight)
}
