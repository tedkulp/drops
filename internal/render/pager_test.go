package render_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// recordingPager stands in for a real pager: it proves a verb handed its
// render to the Pager rather than writing past it.
type recordingPager struct {
	calls int
	out   io.Writer
}

func (pager *recordingPager) Page(out io.Writer, render func(io.Writer) error) error {
	pager.calls++
	pager.out = out
	var paged bytes.Buffer
	if err := render(&paged); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "[paged]%s", paged.String())
	return err
}

func TestRowsRouteThroughThePager(t *testing.T) {
	var out bytes.Buffer
	pager := &recordingPager{}
	console := render.Console{Out: &out, TTY: true, Pager: pager}

	row := render.Row{ID: "k3f9x", Status: model.StatusOpen, Type: model.TypeTask, Title: "one"}
	if err := console.Rows(render.Listing{Rows: []render.Row{row}}); err != nil {
		t.Fatalf("Rows: %v", err)
	}

	if pager.calls != 1 {
		t.Fatalf("pager called %d times, want 1", pager.calls)
	}
	if pager.out != &out {
		t.Errorf("pager was handed %v, want the console's Out", pager.out)
	}
	if got := out.String(); !strings.HasPrefix(got, "[paged]") {
		t.Errorf("rows bypassed the pager: %q", got)
	}
}

func TestPageRoutesThroughThePager(t *testing.T) {
	var out bytes.Buffer
	pager := &recordingPager{}
	console := render.Console{Out: &out, TTY: true, Pager: pager}

	if err := console.Page(render.Page{ID: "k3f9x", Title: "one", Status: model.StatusOpen, Type: model.TypeTask}); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if pager.calls != 1 {
		t.Fatalf("pager called %d times, want 1", pager.calls)
	}
	if got := out.String(); !strings.HasPrefix(got, "[paged]") {
		t.Errorf("page bypassed the pager: %q", got)
	}
}

func TestNoPagerWritesStraightThrough(t *testing.T) {
	var out bytes.Buffer
	console := render.Console{Out: &out}
	row := render.Row{ID: "k3f9x", Status: model.StatusOpen, Type: model.TypeTask, Title: "one"}
	if err := console.Rows(render.Listing{Rows: []render.Row{row}}); err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if got := out.String(); got != "○ k3f9x P0 task     one\n" {
		t.Errorf("unpaged rows: %q", got)
	}
}

// A render error must survive the pager: the pipe still has to close and the
// process still has to be reaped, or the terminal is left to it.
func TestCommandPagerReturnsTheRenderError(t *testing.T) {
	want := errors.New("render failed")
	pager := render.CommandPager{Command: "cat"}
	err := pager.Page(io.Discard, func(io.Writer) error { return want })
	if !errors.Is(err, want) {
		t.Errorf("Page returned %v, want %v", err, want)
	}
}

func TestCommandPagerFeedsARealProcess(t *testing.T) {
	dir := t.TempDir()
	sink := filepath.Join(dir, "seen.txt")
	// A real second process, reading the render off its own stdin. `sh -c`
	// also proves the command line is split into a command and arguments
	// rather than exec'd whole.
	pager := render.CommandPager{Command: "sh -c cat>" + sink}

	if err := pager.Page(io.Discard, func(out io.Writer) error {
		_, err := io.WriteString(out, "rendered through the pager\n")
		return err
	}); err != nil {
		t.Fatalf("Page: %v", err)
	}

	seen, err := os.ReadFile(sink)
	if err != nil {
		t.Fatalf("read what the pager received: %v", err)
	}
	if string(seen) != "rendered through the pager\n" {
		t.Errorf("pager received %q", seen)
	}
}

// Failing to show an issue because `less` is not installed would be a worse
// bug than not paging, so an unstartable pager is not an error.
func TestCommandPagerFallsBackWhenItCannotStart(t *testing.T) {
	var out bytes.Buffer
	pager := render.CommandPager{Command: "drops-has-no-such-pager-binary"}

	if err := pager.Page(&out, func(w io.Writer) error {
		_, err := io.WriteString(w, "still printed\n")
		return err
	}); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if got := out.String(); got != "still printed\n" {
		t.Errorf("fallback wrote %q", got)
	}
}

func TestCommandPagerWithNoCommandWritesStraightThrough(t *testing.T) {
	var out bytes.Buffer
	if err := (render.CommandPager{}).Page(&out, func(w io.Writer) error {
		_, err := io.WriteString(w, "unpaged\n")
		return err
	}); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if got := out.String(); got != "unpaged\n" {
		t.Errorf("empty command wrote %q", got)
	}
}
