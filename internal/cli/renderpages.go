package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// renderPages renders a sequence of pages under one pager, ruled apart. It
// uses render.Page for each issue (which never pagers itself here: the sub
// Console has no Pager), so a multi-id `show` opens one pager, not one per id.
func renderPages(console render.Console, pages []render.Page) error {
	fn := func(w io.Writer) error {
		for n, page := range pages {
			if n > 0 {
				fmt.Fprintf(w, "\n%s\n\n", strings.Repeat("─", console.Width))
			}
			sub := render.Console{Out: w, Width: console.Width, TTY: console.TTY}
			if err := sub.Page(page); err != nil {
				return err
			}
		}
		return nil
	}
	if console.Pager != nil {
		return console.Pager.Page(console.Out, fn)
	}
	return fn(console.Out)
}

// renderComments renders an issue's whole thread under one pager, in the same
// shape render's Page uses for its comments section.
func renderComments(console render.Console, comments []model.Comment) error {
	fn := func(w io.Writer) error {
		if len(comments) == 0 {
			return nil
		}
		fmt.Fprintf(w, "Comments  %d\n", len(comments))
		for _, c := range comments {
			fmt.Fprintln(w)
			header := shortDate(c.CreatedAt)
			if c.Author != "" {
				header += " · " + c.Author
			}
			if c.ID != "" {
				header += " · " + string(c.ID)
			}
			for _, line := range render.Wrap(header, console.Width) {
				fmt.Fprintln(w, line)
			}
			for _, line := range render.WrapText(c.Body, console.Width-2) {
				if line == "" {
					fmt.Fprintln(w)
				} else {
					fmt.Fprintf(w, "  %s\n", line)
				}
			}
		}
		return nil
	}
	if console.Pager != nil {
		return console.Pager.Page(console.Out, fn)
	}
	return fn(console.Out)
}

// shortDate trims a timestamp to its date. A timestamp is opaque text that is
// never parsed and reformatted, so this slices rather than round-tripping.
func shortDate(stamp model.Timestamp) string {
	if len(stamp) < 10 {
		return string(stamp)
	}
	return string(stamp[:10])
}
