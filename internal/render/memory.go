package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/tedkulp/drops/internal/model"
)

// MemoryRow is one memory as the memories scanning verb sees it. A memory has
// no lifecycle status: a live memory borrows the open glyph, while a tombstone
// outranks supersession everywhere it is rendered.
type MemoryRow struct {
	ID           model.ID
	Title        string
	Tombstoned   bool
	SupersededBy *model.ID
}

// MemoryListing is one whole memories result. The ID column is measured across
// the set before any row prints, just as it is for an issue listing.
type MemoryListing struct {
	Rows []MemoryRow
}

// MemoryRows renders a whole memory listing. The ID is never truncated; the
// title and retirement state truncate only when the caller says Out is a
// terminal.
func (console Console) MemoryRows(listing MemoryListing) error {
	measured := memoryLayout{width: console.width(), truncate: console.TTY}
	for _, row := range listing.Rows {
		measured.idPad = max(measured.idPad, Cols(string(row.ID)))
	}
	writeRows := func(out io.Writer) error {
		for _, row := range listing.Rows {
			measured.writeRow(out, row)
		}
		return nil
	}
	return console.paged(writeRows)
}

type memoryLayout struct {
	idPad    int
	width    int
	truncate bool
}

func (measured memoryLayout) writeRow(out io.Writer, row MemoryRow) {
	lead := fmt.Sprintf("%s %-*s", StatusMark(model.StatusOpen, row.Tombstoned), measured.idPad, row.ID)
	display := row.Title
	if state := memoryState(row.Tombstoned, row.SupersededBy); state != "" {
		display += " · " + state
	}
	if measured.truncate {
		display = Ellipsis(display, measured.width-Cols(lead)-2)
	}
	if display == "" {
		fmt.Fprintln(out, lead)
		return
	}
	fmt.Fprintf(out, "%s  %s\n", lead, display)
}

// MemoryPage is one memory in full, as memory show sees it. It preserves the
// command's compact header-and-body shape while making long text wrap and page
// through the same Console as an issue page.
type MemoryPage struct {
	ID           model.ID
	Title        string
	Provenance   string
	Body         string
	Tombstoned   bool
	SupersededBy *model.ID
}

// MemoryPage renders one memory without truncation.
func (console Console) MemoryPage(page MemoryPage) error {
	width := console.width()
	writePage := func(out io.Writer) error {
		var header strings.Builder
		header.WriteString(string(page.ID))
		header.WriteString(" · ")
		header.WriteString(page.Title)
		if page.Provenance != "" {
			header.WriteString(" · from ")
			header.WriteString(page.Provenance)
		}
		if state := memoryState(page.Tombstoned, page.SupersededBy); state != "" {
			header.WriteString(" · ")
			header.WriteString(state)
		}
		writeLines(out, Wrap(header.String(), width))
		writeLines(out, WrapText(page.Body, width))
		return nil
	}
	return console.paged(writePage)
}

func memoryState(tombstoned bool, supersededBy *model.ID) string {
	if tombstoned {
		return "tombstoned"
	}
	if supersededBy != nil {
		return "superseded by " + string(*supersededBy)
	}
	return ""
}
