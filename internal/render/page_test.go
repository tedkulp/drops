package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func renderPage(t *testing.T, console render.Console, page render.Page) string {
	t.Helper()
	if err := console.Page(page); err != nil {
		t.Fatalf("Page: %v", err)
	}
	return console.Out.(*bytes.Buffer).String()
}

func TestPageOpensWithIdentityAndDates(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out}, render.Page{
		ID:        "k3f9x",
		Project:   "drops",
		Title:     "Build the render package",
		Status:    model.StatusInProgress,
		Type:      model.TypeTask,
		Priority:  2,
		Assignee:  "Ted Kulp",
		Labels:    []string{"wayfinder:task", "render"},
		CreatedAt: "2026-09-01T10:04:00Z",
		UpdatedAt: "2026-09-02T08:15:33.501Z",
	})

	const want = "Build the render package\n" +
		"k3f9x · drops · in_progress · task · P2 · @Ted Kulp · wayfinder:task, render\n" +
		"opened 2026-09-01, updated 2026-09-02\n"
	if got != want {
		t.Errorf("page\n got %q\nwant %q", got, want)
	}
}

func TestPageAddsOnlyTheStatesAnIssueIsIn(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out}, render.Page{
		ID:            "k3f9x",
		Project:       "drops",
		Title:         "Build the render package",
		Status:        model.StatusClosed,
		Tombstoned:    true,
		Type:          model.TypeTask,
		Priority:      2,
		CreatedAt:     "2026-09-01T10:04:00Z",
		UpdatedAt:     "2026-09-02T08:15:33Z",
		ClosedAt:      "2026-09-02T08:15:33Z",
		DeferredUntil: "2026-10-01T00:00:00Z",
	})

	const want = "Build the render package\n" +
		"k3f9x · drops · closed · tombstoned · task · P2\n" +
		"opened 2026-09-01, updated 2026-09-02, closed 2026-09-02\n" +
		"deferred until 2026-10-01\n"
	if got != want {
		t.Errorf("page\n got %q\nwant %q", got, want)
	}
}

// An empty description contributes nothing, not a blank line: a thin issue
// costs three lines and no more.
func TestPageOfAThinIssueIsThreeLines(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out}, render.Page{
		ID:        "k3f9x",
		Project:   "drops",
		Title:     "thin",
		Status:    model.StatusOpen,
		Type:      model.TypeTask,
		CreatedAt: "2026-09-01T10:04:00Z",
		UpdatedAt: "2026-09-01T10:04:00Z",
	})
	if lines := strings.Count(got, "\n"); lines != 3 {
		t.Errorf("thin page is %d lines, want 3: %q", lines, got)
	}
}

func TestPageWrapsTheDescriptionUnderABlankLine(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out, Width: 40}, render.Page{
		ID:          "k3f9x",
		Project:     "drops",
		Title:       "t",
		Status:      model.StatusOpen,
		Type:        model.TypeTask,
		CreatedAt:   "2026-09-01T10:04:00Z",
		UpdatedAt:   "2026-09-01T10:04:00Z",
		Description: "## Question\n\nBuild internal/render from scratch over internal/model.",
	})

	const want = "t\n" +
		"k3f9x · drops · open · task · P0\n" +
		"opened 2026-09-01, updated 2026-09-01\n" +
		"\n" +
		"## Question\n" +
		"\n" +
		"Build internal/render from scratch over\n" +
		"internal/model.\n"
	if got != want {
		t.Errorf("page\n got %q\nwant %q", got, want)
	}
}

func TestPageIndentsAWrappedCloseReasonUnderItsLabel(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out, Width: 40}, render.Page{
		ID:          "k3f9x",
		Project:     "drops",
		Title:       "t",
		Status:      model.StatusClosed,
		Type:        model.TypeTask,
		CreatedAt:   "2026-09-01T10:04:00Z",
		UpdatedAt:   "2026-09-01T10:04:00Z",
		ClosedAt:    "2026-09-02T10:04:00Z",
		CloseReason: "cobra stays: wiring is 12.9 percent of the router.\n\nA blank line survives it.",
	})

	const want = "t\n" +
		"k3f9x · drops · closed · task · P0\n" +
		"opened 2026-09-01, updated 2026-09-01, closed 2026-09-02\n" +
		"\n" +
		"closed: cobra stays: wiring is 12.9\n" +
		"        percent of the router.\n" +
		"\n" +
		"        A blank line survives it.\n"
	if got != want {
		t.Errorf("page\n got %q\nwant %q", got, want)
	}
	// A blank line inside an indented block is left bare rather than
	// prefixed: an indented empty line is trailing whitespace.
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == "" && line != "" {
			t.Errorf("line is whitespace only: %q", line)
		}
	}
}

func TestPageNamesEveryRelation(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out, Width: 46}, render.Page{
		ID:        "dw32p.20",
		Project:   "drops",
		Title:     "t",
		Status:    model.StatusOpen,
		Type:      model.TypeTask,
		CreatedAt: "2026-09-01T10:04:00Z",
		UpdatedAt: "2026-09-01T10:04:00Z",
		Parent:    &render.Ref{ID: "dw32p", Title: "Rewrite drops from scratch", Status: model.StatusOpen},
		Blockers: []render.Ref{
			{ID: "dw32p.14", Title: "Build the model package", Status: model.StatusClosed},
		},
		Blocking: []render.Ref{
			{ID: "dw32p.22", Title: "Build the core package", Status: model.StatusOpen},
		},
		Children: []render.Ref{
			{ID: "k3f9x", Title: "one", Status: model.StatusOpen},
			{ID: "beacon-ci-never-executed-4bhg", Title: "two", Status: model.StatusClosed},
			{ID: "br-6vf", Title: "three", Status: model.StatusOpen, Tombstoned: true},
		},
	})

	const want = "t\n" +
		"dw32p.20 · drops · open · task · P0\n" +
		"opened 2026-09-01, updated 2026-09-01\n" +
		"\n" +
		"Parent\n" +
		"  ○ dw32p  Rewrite drops from scratch\n" +
		"\n" +
		"Blocked by\n" +
		"  ● dw32p.14  Build the model package\n" +
		"\n" +
		"Blocks\n" +
		"  ○ dw32p.22  Build the core package\n" +
		"\n" +
		"Children  3, 1 open\n" +
		"  ○ k3f9x                          one\n" +
		"  ● beacon-ci-never-executed-4bhg  two\n" +
		"  ⊘ br-6vf                         three\n"
	if got != want {
		t.Errorf("page\n got %q\nwant %q", got, want)
	}
}

// A page truncates nothing: a ref title too wide for the line wraps under the
// id column, where every byte is still present at a different column.
func TestPageWrapsALongRefTitleUnderItsID(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out, Width: 40}, render.Page{
		ID:        "k3f9x",
		Project:   "drops",
		Title:     "t",
		Status:    model.StatusOpen,
		Type:      model.TypeTask,
		CreatedAt: "2026-09-01T10:04:00Z",
		UpdatedAt: "2026-09-01T10:04:00Z",
		Blockers: []render.Ref{
			{ID: "dw32p.19", Title: "Build the store package over the v7 schema", Status: model.StatusClosed},
		},
	})

	const want = "\nBlocked by\n" +
		"  ● dw32p.19  Build the store package\n" +
		"              over the v7 schema\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("wrapped ref\n got %q\nwant suffix %q", got, want)
	}
	if strings.Contains(got, "…") {
		t.Errorf("a page truncated a ref title: %q", got)
	}
}

func TestPageOmitsRelationBlocksAnIssueHasNone(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out}, render.Page{
		ID: "k3f9x", Project: "drops", Title: "t", Status: model.StatusOpen, Type: model.TypeTask,
		CreatedAt: "2026-09-01T10:04:00Z", UpdatedAt: "2026-09-01T10:04:00Z",
	})
	for _, heading := range []string{"Parent", "Blocked by", "Blocks", "Children"} {
		if strings.Contains(got, heading) {
			t.Errorf("empty %q block printed: %q", heading, got)
		}
	}
}

func TestPageRendersTheWholeThread(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out, Width: 44}, render.Page{
		ID: "k3f9x", Project: "drops", Title: "t", Status: model.StatusOpen, Type: model.TypeTask,
		CreatedAt: "2026-09-01T10:04:00Z", UpdatedAt: "2026-09-01T10:04:00Z",
		Comments: []render.Comment{
			{ID: "k3f9x:0a1b2c3d4e5f", Author: "agent", CreatedAt: "2026-09-01T11:00:00Z",
				Body: "measured against the binary: the frontier query drops the map's own epic."},
			{CreatedAt: "2026-09-02T09:30:00Z", Body: "second"},
		},
	})

	const want = "\nComments  2\n" +
		"\n" +
		"2026-09-01 · agent · k3f9x:0a1b2c3d4e5f\n" +
		"  measured against the binary: the frontier\n" +
		"  query drops the map's own epic.\n" +
		"\n" +
		"2026-09-02\n" +
		"  second\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("thread\n got %q\nwant suffix %q", got, want)
	}
	// A comment with no author and no id gets no dangling separator.
	if strings.Contains(got, "2026-09-02 ·") {
		t.Errorf("dangling separator on a bare comment header: %q", got)
	}
}

func TestPageOmitsTheThreadWhenThereIsNone(t *testing.T) {
	var out bytes.Buffer
	got := renderPage(t, render.Console{Out: &out}, render.Page{
		ID: "k3f9x", Project: "drops", Title: "t", Status: model.StatusOpen, Type: model.TypeTask,
		CreatedAt: "2026-09-01T10:04:00Z", UpdatedAt: "2026-09-01T10:04:00Z",
	})
	if strings.Contains(got, "Comments") {
		t.Errorf("empty thread printed a heading: %q", got)
	}
}

func TestPageWidthIsFixedWhenTheCallerMeasuredNone(t *testing.T) {
	body := strings.TrimSpace(strings.Repeat("word ", 40))
	page := render.Page{
		ID: "k3f9x", Project: "drops", Title: "t", Status: model.StatusOpen, Type: model.TypeTask,
		CreatedAt: "2026-09-01T10:04:00Z", UpdatedAt: "2026-09-01T10:04:00Z", Description: body,
	}

	for _, testcase := range []struct {
		name  string
		width int
		want  int
	}{
		{"unmeasured falls back to the fixed default", 0, render.DefaultWidth},
		{"a width under the floor is not a measurement", 12, render.DefaultWidth},
		{"a very wide terminal is capped", 400, render.MaxWidth},
		{"a measured width in range is used", 55, 55},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			var out bytes.Buffer
			got := renderPage(t, render.Console{Out: &out, Width: testcase.width}, page)
			widest := 0
			for _, line := range strings.Split(got, "\n") {
				if n := len([]rune(line)); n > widest {
					widest = n
				}
			}
			// Greedy wrapping of a uniform 4-letter word fills to within
			// one word of the budget, so the widest line pins the width.
			if widest > testcase.want || widest < testcase.want-5 {
				t.Errorf("widest line is %d columns, want the budget %d", widest, testcase.want)
			}
		})
	}
}
