package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// A frame test is REGRESSION COVER, not a control (map Notes): it fails on any
// byte that moves, so it proves nothing about a specific clause. What it does
// prove is the one thing a two-pane frame can silently get wrong — that every
// finished line is exactly as wide as the terminal, at every width, in every
// mode — which is how euv2e.4's two traps showed up as ragged panes.

// wideRow is a markdown table row. render.WrapText emits one untouched, on
// purpose, so this is how a page comes to hold a line far wider than the pane
// it was rendered for.
const wideRow = "| a path that goes on and on | a rate per gigabyte | a reason this row is far too wide for any pane the split gives it |"

// corpus is a fixture holding one of each thing the layout has to survive.
func corpus(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, "beacon-responsive-layout-pass-6w0g", "iss01", "iss02", "iss03")
	// P0 so it leads the queue: the pane never re-sorts, and the CLI's order
	// is priority ascending, then newest first, then id.
	f.issueWith("The longest open id in the corpus, cut to twelve columns on its row",
		"## A table\n\n"+wideRow+"\n\nAnd a paragraph after it that wraps normally.", 0)
	blocked := f.issue("A blocked issue whose title has to survive the marker", 1)
	blocker := f.issue("The blocker", 2)
	f.blocks(blocked.ID, blocker.ID)
	f.issueIn(f.other, "Another project's issue", 3)
	return f
}

// frameModes is every mode the layout has to hold in, applied to a freshly
// loaded pane.
func frameModes(t *testing.T) []struct {
	name  string
	setup func(*Model)
} {
	t.Helper()
	return []struct {
		name  string
		setup func(*Model)
	}{
		{"at rest", func(*Model) {}},
		{"all projects", func(pane *Model) { press(t, pane, "a") }},
		{"closed included", func(pane *Model) { press(t, pane, "C") }},
		{"filtered", func(pane *Model) { pane.filter = "blocked"; _ = pane.applyFilter() }},
		{"no rows match", func(pane *Model) { pane.filter = "zzzz"; _ = pane.applyFilter() }},
		{"zoomed", func(pane *Model) { press(t, pane, "enter") }},
		{"soft-wrapped", func(pane *Model) { press(t, pane, "w") }},
		{"scrolled right", func(pane *Model) {
			for range 6 {
				press(t, pane, "l")
			}
		}},
		{"help", func(pane *Model) { press(t, pane, "?") }},
		// The picker replaces the detail pane's BODY, keeping the box and
		// its border title, so every frame law has to hold with one open.
		{"picker open", func(pane *Model) { openOnTheBlockedRow(t, pane) }},
		{"followed", func(pane *Model) {
			openOnTheBlockedRow(t, pane)
			pressNamed(t, pane, tea.KeyEnter)
			if len(pane.stack) != 1 {
				t.Fatalf("enter followed nothing; the depth line is not in this frame")
			}
		}},
	}
}

// openOnTheBlockedRow moves to the corpus's one issue with a relation and
// opens the picker on it. `f` on an issue with none is a no-op, which would
// make a frame mode prove nothing.
func openOnTheBlockedRow(t *testing.T, pane *Model) {
	t.Helper()
	press(t, pane, "j")
	press(t, pane, "f")
	if pane.follow == nil {
		t.Fatalf("f opened no picker on %s; this mode proves nothing", pane.detailID)
	}
}

// assertSquare is the whole assertion a frame test makes.
func assertSquare(t *testing.T, what, block string, width, height int) {
	t.Helper()
	lines := strings.Split(block, "\n")
	if len(lines) != height {
		t.Errorf("%s: block is %d lines, want %d", what, len(lines), height)
	}
	for index, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Errorf("%s: line %d measures %d columns, want %d: %q", what, index, got, width, line)
		}
	}
}

func TestEveryFrameLineMeasuresTheFrameWidth(t *testing.T) {
	fixture := corpus(t)

	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}, {60, 12}, {40, 3}} {
		for _, mode := range frameModes(t) {
			pane := fixture.model(size.width, size.height)
			mode.setup(pane)
			assertSquare(t, fmt.Sprintf("%dx%d %s", size.width, size.height, mode.name),
				pane.frame(), size.width, size.height)
		}
	}
}

// TestTheComposedFrameIsSquareBeforeItIsPadded is what stops frame's guard
// masking a ragged pane: compose has to produce the finished geometry itself,
// so a box that measures its own width wrong shows up here rather than being
// squared off one layer up.
func TestTheComposedFrameIsSquareBeforeItIsPadded(t *testing.T) {
	fixture := corpus(t)

	for _, size := range []struct{ width, height int }{{80, 24}, {120, 40}, {60, 12}, {40, 4}} {
		for _, mode := range frameModes(t) {
			pane := fixture.model(size.width, size.height)
			mode.setup(pane)
			assertSquare(t, fmt.Sprintf("composed %dx%d %s", size.width, size.height, mode.name),
				pane.compose(), size.width, size.height)
		}
	}
}

func TestTheDetailPaneTitleCarriesTheFullUntruncatedID(t *testing.T) {
	// This is the whole justification for the left pane's twelve-column id
	// cap: the row truncates what identifies, and the border puts it back.
	fixture := corpus(t)
	pane := fixture.model(80, 24)

	const long = "beacon-responsive-layout-pass-6w0g"
	if pane.detailID != long {
		t.Fatalf("detail pane shows %s, want the long-id issue first", pane.detailID)
	}
	frame := pane.frame()
	if !strings.Contains(frame, "─ "+long+" ─") {
		t.Fatalf("frame has no untruncated id on the detail border:\n%s", frame)
	}
	rows := strings.Split(frame, "\n")[1]
	if !strings.Contains(rows, "beacon-resp…") {
		t.Fatalf("first row = %q, want the same id cut to twelve columns", rows)
	}
}

func TestEnterDropsTheListPaneAndBringsItBack(t *testing.T) {
	fixture := corpus(t)
	pane := fixture.model(80, 24)

	if !strings.Contains(pane.frame(), "─ drops ─") {
		t.Fatal("split frame has no left pane title")
	}
	press(t, pane, "enter")
	frame := pane.frame()
	if strings.Contains(frame, "─ drops ─") {
		t.Fatalf("zoomed frame still carries the list pane:\n%s", frame)
	}
	if !strings.Contains(frame, "zoom detail") {
		t.Fatalf("footer does not disclose the mode:\n%s", frame)
	}

	press(t, pane, "enter")
	if !strings.Contains(pane.frame(), "─ drops ─") {
		t.Fatal("enter did not bring the list pane back")
	}
}

func TestThePickerAndTheDepthLineRenderInsideTheDetailPane(t *testing.T) {
	// The picker takes the detail pane's BODY and keeps qy3de.5's box, so the
	// border title is still the issue you are picking a relation OF.
	fixture := corpus(t)
	pane := fixture.model(80, 24)
	openOnTheBlockedRow(t, pane)

	frame := pane.frame()
	if !strings.Contains(frame, "Blocked by") {
		t.Fatalf("frame carries no picker group header:\n%s", frame)
	}
	if !strings.Contains(frame, "─ iss01 ─") {
		t.Fatalf("the box lost its border title while the picker was open:\n%s", frame)
	}
	if strings.Contains(frame, "A blocked issue whose title has to survive") &&
		!strings.Contains(frame, "The blocker") {
		t.Fatalf("the picker did not replace the page beneath it:\n%s", frame)
	}

	pressNamed(t, pane, tea.KeyEnter)
	frame = pane.frame()
	if !strings.Contains(frame, "← iss01 ·1") {
		t.Fatalf("frame carries no depth line at depth 1:\n%s", frame)
	}
	if !strings.Contains(frame, "─ iss02 ─") {
		t.Fatalf("the border title did not follow the relation:\n%s", frame)
	}
}

func TestClippedBytesAreReachableAndTheFooterSaysSo(t *testing.T) {
	// render.Console.Page truncates nothing, so the pane keeps that promise
	// honestly rather than nominally: the bytes are off-screen, not gone, and
	// the indicator is what stops a clipped table row reading as a whole row.
	fixture := corpus(t)
	pane := fixture.model(80, 24)
	geo := measure(80, 24, false)

	widest := pane.widest()
	if widest <= geo.detailWidth {
		t.Fatalf("page's widest line is %d columns in a %d-column pane; this fixture "+
			"is supposed to overflow", widest, geo.detailWidth)
	}
	if want := "↔ 1–38 of " + strconv.Itoa(widest); !strings.Contains(pane.footer(geo), want) {
		t.Fatalf("footer = %q, want %q", pane.footer(geo), want)
	}

	before := pane.detail.View()
	for range 6 {
		press(t, pane, "l")
	}
	if pane.detail.XOffset() != 6*hscrollStep {
		t.Fatalf("x offset after six presses = %d, want %d", pane.detail.XOffset(), 6*hscrollStep)
	}
	if pane.detail.View() == before {
		t.Fatal("scrolling right changed nothing; the clipped bytes are not reachable")
	}
	if want := "↔ 49–86 of " + strconv.Itoa(widest); !strings.Contains(pane.footer(geo), want) {
		t.Fatalf("footer after scrolling = %q, want %q", pane.footer(geo), want)
	}

	press(t, pane, "0")
	if pane.detail.XOffset() != 0 {
		t.Fatalf("x offset after 0 = %d, want 0", pane.detail.XOffset())
	}
	press(t, pane, "$")
	if pane.detail.XOffset() != widest-geo.detailWidth {
		t.Fatalf("x offset after $ = %d, want %d", pane.detail.XOffset(), widest-geo.detailWidth)
	}
}

func TestSoftWrapReplacesTheScrollIndicator(t *testing.T) {
	fixture := corpus(t)
	pane := fixture.model(80, 24)
	geo := measure(80, 24, false)

	press(t, pane, "w")
	if !pane.detail.SoftWrap {
		t.Fatal("w did not turn soft-wrap on")
	}
	footer := pane.footer(geo)
	if strings.Contains(footer, "↔") {
		t.Fatalf("footer = %q, want no horizontal indicator while wrapped", footer)
	}
	if !strings.Contains(footer, "wrap") {
		t.Fatalf("footer = %q, want the mode disclosed as a word", footer)
	}
}

func TestTheFooterCountsMOfNOnlyWhileSomethingNarrows(t *testing.T) {
	fixture := corpus(t)
	pane := fixture.model(80, 24)
	geo := measure(80, 24, false)

	if footer := pane.footer(geo); !strings.Contains(footer, "drops · 3 issues") {
		t.Fatalf("footer = %q, want a bare count", footer)
	}
	pane.filter = "blocked"
	if err := pane.applyFilter(); err != nil {
		t.Fatalf("apply filter: %v", err)
	}
	if footer := pane.footer(geo); !strings.Contains(footer, "drops · 1 of 3 · /blocked") {
		t.Fatalf("footer = %q, want the narrowed count and the filter", footer)
	}
}

func TestAHeadlessProgramRunsTheFrameAndLeavesTheAltScreen(t *testing.T) {
	// qy3de.2 proved a real tea.Program runs with a pipe and a buffer at a
	// pinned size, so the interactive half needs no terminal and no fake.
	fixture := corpus(t)
	pane := New(fixture.core, fixture.project, "tester")

	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		// Every key that changes state, then quit. Printable keys only: an
		// escape byte written into a pipe would be parsed with whatever
		// follows it.
		// "\r" and "\x7f" are enter and backspace as a terminal sends them,
		// so the follow keys are exercised through bubbletea's own decoder
		// rather than a message this test hand-built.
		for _, key := range []string{
			"j", "j", "k", "G", "g",
			"j", "f", "\r", "\x7f",
			"C", "a", "w", "w", "l", "0", "?", "?", "q",
		} {
			if _, err := writer.Write([]byte(key)); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	var out bytes.Buffer
	err := pane.run(ctx,
		tea.WithInput(reader), tea.WithOutput(&out), tea.WithWindowSize(80, 24))
	if err != nil {
		t.Fatalf("headless run: %v", err)
	}
	rendered := out.String()
	for _, want := range []string{"\x1b[?1049h", "\x1b[?1049l"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("output does not contain %q: the alt screen was not entered and left", want)
		}
	}
	if !strings.Contains(rendered, "drops") {
		t.Fatal("output holds no frame content at all")
	}
}

func TestAViewCarriesTheAltScreenAndTheWindowTitle(t *testing.T) {
	// In bubbletea v2 these ride on the render rather than on program
	// options, so they are observable without running a program at all.
	fixture := corpus(t)
	view := fixture.model(80, 24).View()
	if !view.AltScreen {
		t.Fatal("view.AltScreen is false; a navigator has to own the screen")
	}
	if view.WindowTitle != "drops" {
		t.Fatalf("view.WindowTitle = %q, want drops", view.WindowTitle)
	}
	if view.Content == "" {
		t.Fatal("view carries no content")
	}
}

func TestAResizeRerendersThePageAtTheNewPaneWidth(t *testing.T) {
	fixture := corpus(t)
	pane := fixture.model(80, 24)
	before := pane.pageText

	updated, _ := pane.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	pane = updated.(*Model)
	if pane.err != nil {
		t.Fatalf("resize left an error: %v", pane.err)
	}
	if pane.pageText == before {
		t.Fatal("the page was not re-rendered; it would still be wrapped for a 38-column pane")
	}
	if !strings.Contains(pane.pageText, "The longest open id in the corpus, cut to twelve columns on its row") {
		t.Fatalf("page = %q, want the title on one line in the wider pane", pane.pageText)
	}
	for index, line := range strings.Split(pane.frame(), "\n") {
		if got := lipgloss.Width(line); got != 200 {
			t.Fatalf("after resize, line %d measures %d columns, want 200: %q", index, got, line)
		}
	}
}

func TestTheCursorRowIsTheOnlyStyledRow(t *testing.T) {
	fixture := corpus(t)
	pane := fixture.model(80, 24)

	body := listBody(pane.rows, pane.cursor.position, 38, 5, false, "empty")
	lines := strings.Split(body, "\n")
	if !strings.Contains(lines[0], "\x1b[7m") {
		t.Fatalf("cursor row = %q, want reverse video", lines[0])
	}
	if strings.Contains(lines[1], "\x1b[7m") {
		t.Fatalf("second row = %q, want it unstyled", lines[1])
	}
}

func TestListBodyScrollsToKeepTheCursorVisible(t *testing.T) {
	rows := rowsOf("alpha", "bravo", "charlie", "delta", "echo")
	body := listBody(rows, 4, 30, 2, false, "empty")
	if !strings.Contains(body, "echo") {
		t.Fatalf("window = %q, want the selected row visible", body)
	}
	if strings.Contains(body, "alpha") {
		t.Fatalf("window = %q, want it scrolled past the first row", body)
	}
}
