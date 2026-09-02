package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/tedkulp/drops/internal/model"
)

// Page is one issue in full, as the reading verb sees it. Every optional field
// is absent when it is empty, and nothing on a page is ever truncated.
type Page struct {
	ID         model.ID
	Project    string
	Title      string
	Status     model.Status
	Tombstoned bool
	Type       model.IssueType
	Priority   int
	Assignee   string
	Labels     []string

	CreatedAt     model.Timestamp
	UpdatedAt     model.Timestamp
	ClosedAt      model.Timestamp
	DeferredUntil model.Timestamp

	CloseReason string
	Description string

	Parent   *Ref
	Blockers []Ref
	Blocking []Ref
	Children []Ref
	Comments []Comment
}

// Ref is one related issue, named on a page but not rendered in full.
type Ref struct {
	ID         model.ID
	Title      string
	Status     model.Status
	Tombstoned bool
}

// Comment is one entry in an issue's thread.
type Comment struct {
	ID        model.CommentID
	Author    string
	Body      string
	CreatedAt model.Timestamp
}

// closeIndent hangs a wrapped close reason under its "closed: " label.
const closeIndent = "        "

// refGutter is the two-column gutter, the glyph and its space; refGap is the
// two columns between a ref's id and its title.
const (
	refGutter = 4
	refGap    = 2
)

// Page writes the whole single-issue view: identity, then body, then
// relations, then comments — what the issue IS before what it is attached to,
// because a reader who already chose this issue wants to read it rather than
// re-identify it.
//
// Nothing here truncates. A page pages, so a 28-child epic costs a scroll
// rather than a second command, and no relation is ever silently withheld.
func (console Console) Page(page Page) error {
	width := console.width()
	return console.paged(func(out io.Writer) error {
		page.writeIdentity(out, width)
		page.writeBody(out, width)
		page.writeRelations(out, width)
		writeComments(out, page.Comments, width)
		return nil
	})
}

func (page Page) writeIdentity(out io.Writer, width int) {
	// The title wraps too. A terminal soft-wrapping it breaks wherever the
	// edge happens to fall, mid-word.
	writeLines(out, Wrap(page.Title, width))

	strip := []string{string(page.ID), page.Project, string(page.Status)}
	if page.Tombstoned {
		strip = append(strip, "tombstoned")
	}
	strip = append(strip, string(page.Type), fmt.Sprintf("P%d", page.Priority))
	if page.Assignee != "" {
		strip = append(strip, "@"+page.Assignee)
	}
	if len(page.Labels) > 0 {
		strip = append(strip, strings.Join(page.Labels, ", "))
	}
	// Wrapped like anything else. With a 34-character migrated id, a long
	// project slug and several labels, the strip outgrows 80 columns, and it
	// is the one line a reader uses to identify what they are looking at.
	writeLines(out, Wrap(strings.Join(strip, " · "), width))

	dates := "opened " + shortDate(page.CreatedAt) + ", updated " + shortDate(page.UpdatedAt)
	if page.ClosedAt != "" {
		dates += ", closed " + shortDate(page.ClosedAt)
	}
	fmt.Fprintln(out, dates)
	if page.DeferredUntil != "" {
		fmt.Fprintln(out, "deferred until "+shortDate(page.DeferredUntil))
	}
}

func (page Page) writeBody(out io.Writer, width int) {
	// A close reason goes through the same wrap as the body, not printed raw.
	// It is a resolution, and a resolution runs to thousands of characters:
	// `close --reason` is the only place one can go.
	if strings.TrimSpace(page.CloseReason) != "" {
		fmt.Fprintln(out)
		writeHanging(out, "closed: ", closeIndent, WrapText(page.CloseReason, width-len(closeIndent)))
	}
	// An empty description contributes nothing, not a blank line: a thin
	// issue costs three lines, the identity strip and its dates.
	if strings.TrimSpace(page.Description) != "" {
		fmt.Fprintln(out)
		writeLines(out, WrapText(page.Description, width))
	}
}

func (page Page) writeRelations(out io.Writer, width int) {
	if page.Parent != nil {
		writeRefs(out, "Parent", []Ref{*page.Parent}, width)
	}
	writeRefs(out, "Blocked by", page.Blockers, width)
	writeRefs(out, "Blocks", page.Blocking, width)
	if len(page.Children) > 0 {
		writeRefs(out, fmt.Sprintf("Children  %d, %d open",
			len(page.Children), openRefs(page.Children)), page.Children, width)
	}
}

// writeRefs writes one block of related issues under a heading.
//
// The id column sizes to the widest id present rather than to a fixed pad, the
// same rule a listing's geometry follows, so the two renderers cannot disagree
// about how an id column is sized: migrated ids run to 34 characters, which a
// fixed pad renders ragged.
//
// A title too wide for the line WRAPS under the id rather than being cut. That
// is the one place this differs from a listing, and deliberately: a page
// truncates nothing, and wrapping is lossless where truncation is not.
func writeRefs(out io.Writer, heading string, refs []Ref, width int) {
	if len(refs) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s\n", heading)
	pad := 0
	for _, ref := range refs {
		if n := cols(string(ref.ID)); n > pad {
			pad = n
		}
	}
	indent := strings.Repeat(" ", refGutter+pad+refGap)
	for _, ref := range refs {
		label := fmt.Sprintf("  %s %-*s  ", StatusMark(ref.Status, ref.Tombstoned), pad, ref.ID)
		writeHanging(out, label, indent, Wrap(ref.Title, width-cols(indent)))
	}
}

// writeComments writes a whole thread, oldest first, each body wrapped and
// indented two columns so a comment is distinguishable from the issue's own
// prose.
//
// The whole thread, never a preview — the same reason no relation block is
// truncated. The count still leads, because "how much discussion is here" is
// worth answering before reading any of it. Each comment's id is on its header
// line rather than omitted as redundant: it is the one argument `comment rm`
// takes, and the header is where a reader copies it from.
func writeComments(out io.Writer, comments []Comment, width int) {
	if len(comments) == 0 {
		return
	}
	fmt.Fprintf(out, "\nComments  %d\n", len(comments))
	for _, comment := range comments {
		fmt.Fprintln(out)
		header := shortDate(comment.CreatedAt)
		if comment.Author != "" {
			header += " · " + comment.Author
		}
		// Guarded rather than appended unconditionally: a dangling " ·" on a
		// comment that has no id is a rendering bug waiting for its first row.
		if comment.ID != "" {
			header += " · " + string(comment.ID)
		}
		writeLines(out, Wrap(header, width))
		writeHanging(out, "  ", "  ", WrapText(comment.Body, width-2))
	}
}

// openRefs counts the refs still outstanding. A tombstoned ref is not open
// whatever its status says.
func openRefs(refs []Ref) int {
	open := 0
	for _, ref := range refs {
		if !ref.Tombstoned && !ref.Status.IsTerminal() {
			open++
		}
	}
	return open
}

// writeHanging writes the first line after label and every line after it under
// indent. A blank line is left bare rather than prefixed, because an indented
// empty line is nothing but trailing whitespace.
func writeHanging(out io.Writer, label, indent string, lines []string) {
	if len(lines) == 0 {
		fmt.Fprintln(out, strings.TrimRight(label, " "))
		return
	}
	for index, line := range lines {
		switch {
		case line == "":
			fmt.Fprintln(out)
		case index == 0:
			fmt.Fprintf(out, "%s%s\n", label, line)
		default:
			fmt.Fprintf(out, "%s%s\n", indent, line)
		}
	}
}

func writeLines(out io.Writer, lines []string) {
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
}

// shortDate trims a timestamp to its date. A timestamp is opaque text that
// must never be parsed and reformatted, so this slices rather than
// round-tripping through time.Parse.
func shortDate(stamp model.Timestamp) string {
	if len(stamp) < 10 {
		return string(stamp)
	}
	return string(stamp[:10])
}
