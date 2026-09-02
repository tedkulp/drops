package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func TestRowsWritesOneRowPerIssue(t *testing.T) {
	var out bytes.Buffer
	console := render.Console{Out: &out}

	err := console.Rows(render.Listing{Rows: []render.Row{
		{ID: "k3f9x", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: "Build the render package"},
		{ID: "dw32p", Status: model.StatusClosed, Type: model.TypeEpic, Priority: 0, Title: "Rewrite drops from scratch"},
	}})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	const want = "○ k3f9x P2 task     Build the render package\n" +
		"● dw32p P0 epic     Rewrite drops from scratch\n"
	if got := out.String(); got != want {
		t.Errorf("rows output\n got %q\nwant %q", got, want)
	}
}

// longTitle is 60 runes, four of them multi-byte, so a byte-counting
// truncation would cut it in a visibly different place.
const longTitle = "Rewrite drops — the whole store, the mirror — from scratch."

func TestRowsTruncateOnlyOnATerminal(t *testing.T) {
	row := render.Row{ID: "k3f9x", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: longTitle}

	t.Run("terminal cuts the title to the width", func(t *testing.T) {
		var out bytes.Buffer
		console := render.Console{Out: &out, TTY: true, Width: 40}
		if err := console.Rows(render.Listing{Rows: []render.Row{row}}); err != nil {
			t.Fatalf("Rows: %v", err)
		}
		const want = "○ k3f9x P2 task     Rewrite drops — the…\n"
		if got := out.String(); got != want {
			t.Errorf("truncated row\n got %q\nwant %q", got, want)
		}
		if width := len([]rune(strings.TrimSuffix(out.String(), "\n"))); width != 40 {
			t.Errorf("truncated row is %d columns, want 40", width)
		}
	})

	t.Run("no terminal prints every byte", func(t *testing.T) {
		var out bytes.Buffer
		console := render.Console{Out: &out, TTY: false, Width: 40}
		if err := console.Rows(render.Listing{Rows: []render.Row{row}}); err != nil {
			t.Fatalf("Rows: %v", err)
		}
		if got := out.String(); !strings.HasSuffix(got, longTitle+"\n") {
			t.Errorf("redirected row dropped title bytes: %q", got)
		}
	})
}

func TestRowsSizeTheIDColumnToTheWidestID(t *testing.T) {
	var out bytes.Buffer
	console := render.Console{Out: &out}

	err := console.Rows(render.Listing{Rows: []render.Row{
		{ID: "k3f9x", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: "short id"},
		{ID: "beacon-ci-never-executed-4bhg", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: "migrated id"},
	}})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 rows, got %d: %q", len(lines), out.String())
	}
	// Both titles start at the same column, and the column is set by the
	// widest id present rather than by a fixed pad.
	first, second := strings.Index(lines[0], "short id"), strings.Index(lines[1], "migrated id")
	if first != second {
		t.Errorf("titles start at columns %d and %d, want the same", first, second)
	}
	if !strings.Contains(lines[0], "k3f9x"+strings.Repeat(" ", len("beacon-ci-never-executed-4bhg")-len("k3f9x"))+" P2") {
		t.Errorf("short id is not padded to the widest id: %q", lines[0])
	}
}

func TestRowsShowTheProjectColumnOnlyWhenAsked(t *testing.T) {
	rows := []render.Row{
		{ID: "k3f9x", Project: "drops", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: "one"},
		{ID: "br-6vf", Project: "global", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: "two"},
	}

	var off bytes.Buffer
	if err := (render.Console{Out: &off}).Rows(render.Listing{Rows: rows}); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if strings.Contains(off.String(), "drops") || strings.Contains(off.String(), "global") {
		t.Errorf("project column printed without Listing.Project: %q", off.String())
	}

	var on bytes.Buffer
	if err := (render.Console{Out: &on}).Rows(render.Listing{Rows: rows, Project: true}); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	const want = "○ k3f9x  P2 task     drops  one\n" +
		"○ br-6vf P2 task     global two\n"
	if got := on.String(); got != want {
		t.Errorf("project column\n got %q\nwant %q", got, want)
	}
}

func TestRowsIndentAContinuationFourColumns(t *testing.T) {
	var out bytes.Buffer
	console := render.Console{Out: &out}

	err := console.Rows(render.Listing{Rows: []render.Row{
		{ID: "k3f9x", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: "one", Note: "blocked by dw32p.14, dw32p.19"},
		{ID: "br-6vf", Status: model.StatusOpen, Type: model.TypeTask, Priority: 2, Title: "two"},
	}})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	const want = "○ k3f9x  P2 task     one\n" +
		"    blocked by dw32p.14, dw32p.19\n" +
		"○ br-6vf P2 task     two\n"
	if got := out.String(); got != want {
		t.Errorf("continuation\n got %q\nwant %q", got, want)
	}
}

func TestRowsMarkEveryState(t *testing.T) {
	for _, testcase := range []struct {
		name       string
		status     model.Status
		tombstoned bool
		want       string
	}{
		{"open", model.StatusOpen, false, "○"},
		{"in progress", model.StatusInProgress, false, "◐"},
		{"closed", model.StatusClosed, false, "●"},
		{"tombstoned outranks its status", model.StatusOpen, true, "⊘"},
		{"tombstoned outranks closed too", model.StatusClosed, true, "⊘"},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var out bytes.Buffer
			console := render.Console{Out: &out}
			row := render.Row{ID: "k3f9x", Status: testcase.status, Tombstoned: testcase.tombstoned, Type: model.TypeTask, Title: "t"}
			if err := console.Rows(render.Listing{Rows: []render.Row{row}}); err != nil {
				t.Fatalf("Rows: %v", err)
			}
			if got := strings.SplitN(out.String(), " ", 2)[0]; got != testcase.want {
				t.Errorf("glyph for %s: got %q, want %q", testcase.name, got, testcase.want)
			}
		})
	}
}
