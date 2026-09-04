package render

import (
	"fmt"
	"io"

	"github.com/tedkulp/drops/internal/model"
)

// A SCANNING verb renders one row per issue: list, ready, blocked and search.
// A READING verb renders one issue in full: show. The split decides what each
// may drop, and the rule is not the same on both sides. A scanning verb
// truncates what it DISPLAYS — a title is a label you recognise, and a
// shortened one still does that job — but never what IDENTIFIES, because an id
// is a key you paste and a truncated id cannot be pasted. A page truncates
// nothing at all.

// Row is one issue as a scanning verb sees it: the columns, plus the one line
// of continuation a verb may want under them.
type Row struct {
	ID       model.ID
	Project  string
	Status   model.Status
	Type     model.IssueType
	Priority int
	Title    string
	// Tombstoned outranks Status in the glyph, because a removed issue that
	// reads as open is worse than one whose lifecycle is hidden.
	Tombstoned bool
	// Note is the continuation line under this row: `ready`'s "unblocks 3",
	// `blocked`'s "blocked by a, b". Render owns the indent; the caller owns
	// the words, so a new continuation needs no change here.
	Note string
}

// Listing is one whole scanning-verb result. Geometry is measured across the
// set before any row prints, so the caller hands over all of it at once.
type Listing struct {
	Rows []Row
	// Project turns on the project column. It is off within one project,
	// where the value is constant and the column is pure lost width, and on
	// under --all-projects, where it is unrecoverable from the row: `move`
	// freezes an id across a project change, so a prefix records where an
	// issue was minted, not where it lives.
	Project bool
}

// Rows renders a whole listing, one row per issue.
func (console Console) Rows(listing Listing) error {
	layout := console.measure(listing)
	return console.paged(func(out io.Writer) error {
		for _, row := range listing.Rows {
			layout.writeRow(out, row)
		}
		return nil
	})
}

// layout is the column geometry for one listing, measured once from the whole
// result set before any row prints.
type layout struct {
	idPad    int
	projPad  int // zero when the project column is off
	width    int
	truncate bool
}

// measure sizes the columns to the widest value present.
//
// truncate is decided from Console.TTY and never from the writer a row is
// finally written to: a pager hands the render function a pipe, which is not a
// terminal, so a check made down there would disable truncation in exactly the
// case it exists for.
func (console Console) measure(listing Listing) layout {
	measured := layout{width: console.width(), truncate: console.TTY}
	for _, row := range listing.Rows {
		if n := Cols(string(row.ID)); n > measured.idPad {
			measured.idPad = n
		}
		if listing.Project {
			if n := Cols(row.Project); n > measured.projPad {
				measured.projPad = n
			}
		}
	}
	return measured
}

// writeRow renders one issue as one row, and its continuation if it has one.
//
// One issue is always exactly one row: a title that does not fit is cut, never
// wrapped, because a wrapped listing stops being scannable.
func (measured layout) writeRow(out io.Writer, row Row) {
	lead := fmt.Sprintf("%s %-*s P%d %-8s",
		StatusMark(row.Status, row.Tombstoned), measured.idPad, row.ID, row.Priority, row.Type)
	if measured.projPad > 0 {
		lead += fmt.Sprintf(" %-*s", measured.projPad, row.Project)
	}
	title := row.Title
	if measured.truncate {
		title = Ellipsis(title, measured.width-Cols(lead)-1)
	}
	// No trailing space when nothing is left to print, so a row that is all
	// gutter still ends where its last column does.
	if title == "" {
		fmt.Fprintln(out, lead)
	} else {
		fmt.Fprintf(out, "%s %s\n", lead, title)
	}
	if row.Note != "" {
		fmt.Fprintf(out, "    %s\n", row.Note)
	}
}

// StatusMark is the one-glyph status vocabulary shared by issue rows, issue
// pages, related issues and memory rows, so those surfaces cannot drift.
// Nothing parses these: every scripted consumer reads --json.
func StatusMark(status model.Status, tombstoned bool) string {
	if tombstoned {
		return "⊘"
	}
	switch status {
	case model.StatusClosed:
		return "●"
	case model.StatusInProgress:
		return "◐"
	}
	return "○"
}
