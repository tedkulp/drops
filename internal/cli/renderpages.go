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

// renderComments renders an issue's whole thread through render's own thread
// writer — the one `show` uses — so `comment list` and a page can never
// disagree about what a thread looks like.
func renderComments(console render.Console, comments []model.Comment) error {
	thread := make([]render.Comment, 0, len(comments))
	for _, c := range comments {
		thread = append(thread, render.Comment{
			ID: c.ID, Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt,
		})
	}
	return console.Thread(thread)
}
