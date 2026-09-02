package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Report writes the enumerated record AGENTS.md asks a ticket to carry: for
// each control, the file and the behaviour, the test and whether it went red,
// and confirmation that the file was restored.
//
// It is written as controls finish rather than at the end, so a long run shows
// its findings as it goes and an interrupted one has still said what it proved.
type Report struct {
	W       io.Writer
	lastPkg string
}

// Control writes one control's record.
func (r *Report) Control(res Result) {
	c := res.Control
	if pkg := strings.TrimPrefix(c.pkg(), "./"); pkg != r.lastPkg {
		if r.lastPkg != "" {
			fmt.Fprintln(r.W)
		}
		fmt.Fprintf(r.W, "%s\n\n", pkg)
		r.lastPkg = pkg
	}

	mark := "✗"
	if res.Outcome.ok() {
		mark = "✓"
	}
	fmt.Fprintf(r.W, "  %s %-40s %11s  %s\n", mark, c.Name, res.Outcome, res.Elapsed.Round(time.Millisecond))
	field(r.W, "clause", c.Clause)
	field(r.W, "control", fmt.Sprintf("%s\n%s  →  %s", c.File, quote(c.Old), quote(c.New)))
	field(r.W, "test", c.Test+testNote(res.Outcome))
	if res.Detail != "" {
		field(r.W, "", res.Detail)
	}
	field(r.W, "restore", restoreLine(res))
	fmt.Fprintln(r.W)
}

// testNote puts the verdict on the test's own line, so the two things a record
// is read for — which test, and did it go red — sit together.
func testNote(o Outcome) string {
	if o == Red {
		return " — went red under the mutation"
	}
	return ""
}

func restoreLine(res Result) string {
	if res.Sum == "" {
		return "not reached — the file was never mutated"
	}
	if !res.Restored {
		return "FAILED — sha256:" + res.Sum[:12] + " is NOT back in the tree"
	}
	return "sha256:" + res.Sum[:12] + " verified back in place"
}

// Summary closes the record with the counts and returns whether every control
// was proved.
func (r *Report) Summary(results []Result) bool {
	red, findings := 0, 0
	for _, res := range results {
		if res.Outcome.ok() {
			red++
		} else {
			findings++
		}
	}
	fmt.Fprintf(r.W, "%d %s · %d red · %d %s\n",
		len(results), plural(len(results), "control"), red, findings, plural(findings, "finding"))
	if findings > 0 {
		fmt.Fprintln(r.W, "\nA finding is a test that cannot fail for the clause it appears to cover.")
	}
	return findings == 0
}

// plural gives a noun the ending a count of n wants, so a one-control run does
// not read "1 controls".
func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// field prints a labelled block, wrapping continuations under the value column
// so a multi-line clause or a compiler diagnostic stays inside its own field.
func field(w io.Writer, label, value string) {
	const indent = "             "
	for i, line := range strings.Split(strings.TrimRight(value, "\n"), "\n") {
		if i == 0 {
			fmt.Fprintf(w, "    %-9s%s\n", label, line)
			continue
		}
		fmt.Fprintf(w, "%s%s\n", indent, line)
	}
}

// quote renders a mutation's text on one line. A newline inside it is shown as
// \n rather than breaking the arrow across lines, because the two halves of a
// mutation only read as a mutation side by side.
func quote(s string) string {
	return "`" + strings.ReplaceAll(strings.ReplaceAll(s, "\n", `\n`), "\t", `\t`) + "`"
}
