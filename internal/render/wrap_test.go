package render_test

import (
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/render"
)

func TestWrapTextReflowsAParagraphAsAWhole(t *testing.T) {
	// Authored hard-wrapped at some other width. Wrapping line by line
	// spills exactly one word off each source line, leaving a one-word
	// orphan on alternating lines; reflowing the paragraph does not.
	body := "the store owned every transaction\nboundary and that is why the import\nrules were stuck inside it"

	// Rendered wider than it was authored: reflow repacks the paragraph,
	// where a line-by-line wrap would leave the source's own breaks in place.
	got := render.WrapText(body, 60)

	want := []string{
		"the store owned every transaction boundary and that is why",
		"the import rules were stuck inside it",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("reflowed\n got %q\nwant %q", got, want)
	}
}

func TestWrapTextCountsRunesNotBytes(t *testing.T) {
	// Nine em dashes: 27 bytes, 17 runes with the spaces. A byte count wraps
	// this to three lines against a width of 20; a rune count does not.
	got := render.WrapText(strings.TrimSpace(strings.Repeat("— ", 9)), 20)
	if len(got) != 1 {
		t.Errorf("em dashes wrapped into %d lines, want 1: %q", len(got), got)
	}
}

func TestWrapTextLeavesStructureAlone(t *testing.T) {
	for _, testcase := range []struct {
		name string
		line string
	}{
		{"heading", "## The packages, and what each hides, at some length"},
		{"quote", "> a quoted line that runs past the width it is given"},
		{"table row", "| column | another column | a third column | more |"},
		{"backtick fence", "```sh"},
		{"tilde fence", "~~~"},
		{"thematic break", "---"},
		{"indented code", "    drops ready -t task --json | jq --arg m dw32p."},
		{"tab-indented code", "\tdrops ready -t task --json | jq --arg m dw32p."},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			got := render.WrapText(testcase.line, 20)
			if len(got) != 1 || got[0] != testcase.line {
				t.Errorf("%s was reflowed: %q", testcase.name, got)
			}
		})
	}
}

// A structural line must not absorb the prose under it either: joined into the
// paragraph below, a rule renders as "--- the next sentence".
func TestWrapTextStructureDoesNotAbsorbTheProseUnderIt(t *testing.T) {
	got := render.WrapText("---\nthe next sentence", 40)
	want := []string{"---", "the next sentence"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapTextKeepsBlankLinesBetweenParagraphs(t *testing.T) {
	got := render.WrapText("first\n\nsecond", 40)
	want := []string{"first", "", "second"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapTextHangsAListItemUnderItsMarker(t *testing.T) {
	body := "- the store gives up transaction ownership\n  and core composes transactions above it\n- resolve becomes a pure function"

	got := render.WrapText(body, 34)

	want := []string{
		"- the store gives up transaction",
		"  ownership and core composes",
		"  transactions above it",
		"- resolve becomes a pure function",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("list item\n got %q\nwant %q", got, want)
	}
}

func TestWrapTextTellsAMarkerFromProse(t *testing.T) {
	for _, testcase := range []struct {
		name   string
		line   string
		isItem bool
	}{
		{"dash marker", "- an item", true},
		{"star marker", "* an item", true},
		{"plus marker", "+ an item", true},
		{"ordered marker", "1. an item", true},
		{"parenthesised ordered marker", "12) an item", true},
		{"emphasis is not a marker", "*bold* text that keeps going", false},
		{"an em rule is not a marker", "--- and more", false},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			// A marker hangs its continuation two columns; prose does not.
			got := render.WrapText(testcase.line+" "+strings.Repeat("word ", 10), 20)
			hung := len(got) > 1 && strings.HasPrefix(got[1], "  ")
			if hung != testcase.isItem {
				t.Errorf("hanging indent %v, want %v: %q", hung, testcase.isItem, got)
			}
		})
	}
}
