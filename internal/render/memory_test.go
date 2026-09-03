package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func TestMemoryRowsRenderTheMemoryShapeAndState(t *testing.T) {
	var out bytes.Buffer
	supersededBy := model.ID("mem-y2v6q")
	listing := render.MemoryListing{Rows: []render.MemoryRow{
		{ID: "mem-k3f9x", Title: "current note"},
		{ID: "gitops-r1w", Title: "removed note", Tombstoned: true, SupersededBy: &supersededBy},
		{ID: "mem-p4h7x", Title: "stale note", SupersededBy: &supersededBy},
	}}

	if err := (render.Console{Out: &out}).MemoryRows(listing); err != nil {
		t.Fatalf("MemoryRows: %v", err)
	}
	const want = "○ mem-k3f9x   current note\n" +
		"⊘ gitops-r1w  removed note · tombstoned\n" +
		"○ mem-p4h7x   stale note · superseded by mem-y2v6q\n"
	if got := out.String(); got != want {
		t.Errorf("memory rows output\n got %q\nwant %q", got, want)
	}
}

func TestMemoryRowsTruncateOnlyOnATerminal(t *testing.T) {
	row := render.MemoryRow{ID: "mem-k3f9x", Title: "a deliberately long memory title"}

	var terminal bytes.Buffer
	if err := (render.Console{Out: &terminal, TTY: true, Width: 30}).MemoryRows(render.MemoryListing{Rows: []render.MemoryRow{row}}); err != nil {
		t.Fatalf("terminal MemoryRows: %v", err)
	}
	if got, want := terminal.String(), "○ mem-k3f9x  a deliberately l…\n"; got != want {
		t.Errorf("terminal memory row = %q, want %q", got, want)
	}

	var pipe bytes.Buffer
	if err := (render.Console{Out: &pipe, Width: 30}).MemoryRows(render.MemoryListing{Rows: []render.MemoryRow{row}}); err != nil {
		t.Fatalf("non-terminal MemoryRows: %v", err)
	}
	if got, want := pipe.String(), "○ mem-k3f9x  a deliberately long memory title\n"; got != want {
		t.Errorf("non-terminal memory row = %q, want %q", got, want)
	}
}

func TestMemoryPageWrapsWithoutChangingItsShape(t *testing.T) {
	var out bytes.Buffer
	page := render.MemoryPage{
		ID:         "mem-k3f9x",
		Title:      "a long memory title",
		Provenance: "wayfinder",
		Body:       "This memory body has enough words to wrap at thirty columns without losing any text.",
	}

	if err := (render.Console{Out: &out, Width: 30}).MemoryPage(page); err != nil {
		t.Fatalf("MemoryPage: %v", err)
	}
	const want = "mem-k3f9x · a long memory\n" +
		"title · from wayfinder\n" +
		"This memory body has enough\n" +
		"words to wrap at thirty\n" +
		"columns without losing any\n" +
		"text.\n"
	if got := out.String(); got != want {
		t.Errorf("memory page output\n got %q\nwant %q", got, want)
	}
}

func TestMemoryPageReportsTombstoneBeforeSupersession(t *testing.T) {
	var out bytes.Buffer
	supersededBy := model.ID("mem-y2v6q")
	page := render.MemoryPage{
		ID:           "mem-k3f9x",
		Title:        "removed note",
		Body:         "removed body",
		Tombstoned:   true,
		SupersededBy: &supersededBy,
	}

	if err := (render.Console{Out: &out}).MemoryPage(page); err != nil {
		t.Fatalf("MemoryPage: %v", err)
	}
	if got, want := out.String(), "mem-k3f9x · removed note · tombstoned\nremoved body\n"; got != want {
		t.Errorf("memory page = %q, want %q", got, want)
	}
}

func TestMemorySurfacesRouteThroughThePager(t *testing.T) {
	var out bytes.Buffer
	pager := &recordingPager{}
	console := render.Console{Out: &out, TTY: true, Pager: pager}

	if err := console.MemoryRows(render.MemoryListing{Rows: []render.MemoryRow{{ID: "mem-k3f9x", Title: "one"}}}); err != nil {
		t.Fatalf("MemoryRows: %v", err)
	}
	if err := console.MemoryPage(render.MemoryPage{ID: "mem-k3f9x", Title: "one", Body: "body"}); err != nil {
		t.Fatalf("MemoryPage: %v", err)
	}
	if pager.calls != 2 {
		t.Fatalf("pager called %d times, want once per memory surface", pager.calls)
	}
	if got := out.String(); strings.Count(got, "[paged]") != 2 {
		t.Errorf("a memory surface bypassed the pager: %q", got)
	}
}
