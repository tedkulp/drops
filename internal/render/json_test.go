package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/render"
)

// An empty result is [], never null: consumers pipe this straight into
// `jq -r '.[].id'`, which fails on null.
func TestEmitManyWritesAnEmptyArrayForAnEmptyResult(t *testing.T) {
	var out bytes.Buffer
	if err := render.EmitMany(&out, []render.Row(nil)); err != nil {
		t.Fatalf("EmitMany: %v", err)
	}
	if got := out.String(); got != "[]\n" {
		t.Errorf("empty result: got %q, want %q", got, "[]\n")
	}
}

func TestEmitManyWritesABareArrayWithNoEnvelope(t *testing.T) {
	var out bytes.Buffer
	type row struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := render.EmitMany(&out, []row{{ID: "k3f9x", Title: "one"}, {ID: "br-6vf", Title: "two"}}); err != nil {
		t.Fatalf("EmitMany: %v", err)
	}
	const want = `[{"id":"k3f9x","title":"one"},{"id":"br-6vf","title":"two"}]` + "\n"
	if got := out.String(); got != want {
		t.Errorf("array\n got %q\nwant %q", got, want)
	}
}

func TestEmitOneWritesABareObjectWithNoEnvelope(t *testing.T) {
	var out bytes.Buffer
	type report struct {
		Moved []string `json:"moved"`
		To    string   `json:"to"`
	}
	if err := render.EmitOne(&out, report{To: "drops"}); err != nil {
		t.Fatalf("EmitOne: %v", err)
	}
	// A nil list inside the value is [] as well, which is what makes
	// `.moved[]` safe on every result rather than on most of them.
	const want = `{"moved":[],"to":"drops"}` + "\n"
	if got := out.String(); got != want {
		t.Errorf("object\n got %q\nwant %q", got, want)
	}
}

// Issue bodies are full of code. Escaping < > & would make every one of them
// unreadable, and it would not be byte-stable against the mirror either.
func TestEmitLeavesMarkupUnescaped(t *testing.T) {
	var out bytes.Buffer
	type row struct {
		Body string `json:"body"`
	}
	if err := render.EmitOne(&out, row{Body: "if a < b && c > d { return \"x\" }"}); err != nil {
		t.Fatalf("EmitOne: %v", err)
	}
	got := out.String()
	for _, escape := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(got, escape) {
			t.Errorf("markup was escaped as %s: %q", escape, got)
		}
	}
	if want := `{"body":"if a < b && c > d { return \"x\" }"}` + "\n"; got != want {
		t.Errorf("body\n got %q\nwant %q", got, want)
	}
}

// Exactly one trailing newline, so a `--json` result is one line a shell can
// read and two runs concatenate cleanly.
func TestEmitEndsWithExactlyOneNewline(t *testing.T) {
	var out bytes.Buffer
	if err := render.EmitMany(&out, []int{1, 2}); err != nil {
		t.Fatalf("EmitMany: %v", err)
	}
	if err := render.EmitOne(&out, struct{}{}); err != nil {
		t.Fatalf("EmitOne: %v", err)
	}
	if got := out.String(); got != "[1,2]\n{}\n" {
		t.Errorf("concatenated emissions: %q", got)
	}
}
