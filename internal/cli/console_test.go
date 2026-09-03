package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/render"
)

// TestColumnsPinsTheWrapWithoutStartingTruncation is the exact asymmetry the
// doc spells out: $COLUMNS wins over the measurement so a caller can pin the
// width with or without a terminal, but truncation is gated on TERMINAL-NESS
// alone. Pinning $COLUMNS off a terminal therefore changes wrapping and never
// starts truncating — which is what keeps `drops list | grep` lossless even
// when a caller has pinned a narrow width.
func TestColumnsPinsTheWrapWithoutStartingTruncation(t *testing.T) {
	db, cwd := newStore(t)
	body := "A paragraph long enough that the width it is wrapped at is visible in " +
		"where its line breaks fall rather than having to be inferred."
	title := "a title far longer than forty columns so a truncating renderer would have to cut it"
	id := mustRun(t, db, cwd, "create", title, "--inbox", "-d", body)

	t.Setenv("COLUMNS", "40")
	narrow := mustRun(t, db, cwd, "show", id)
	t.Setenv("COLUMNS", "100")
	wide := mustRun(t, db, cwd, "show", id)

	if longestLine(narrow) > 40 {
		t.Errorf("$COLUMNS=40 did not pin the wrap: longest line is %d columns:\n%s", longestLine(narrow), narrow)
	}
	if longestLine(wide) <= 40 {
		t.Errorf("$COLUMNS=100 wrapped as if it were 40:\n%s", wide)
	}

	// Both still contain every word: a page truncates nothing at any width.
	for _, page := range []string{narrow, wide} {
		for _, word := range strings.Fields(body) {
			if !strings.Contains(page, word) {
				t.Fatalf("wrapping dropped %q:\n%s", word, page)
			}
		}
	}

	// And the scanning verb is unmoved: off a terminal it truncates at no
	// width at all, pinned or otherwise.
	t.Setenv("COLUMNS", "40")
	if row := mustRun(t, db, cwd, "list", "--inbox"); !strings.Contains(row, title) {
		t.Fatalf("a pinned $COLUMNS started truncating a listing off a terminal:\n%s", row)
	}
}

func longestLine(s string) int {
	longest := 0
	for _, line := range strings.Split(s, "\n") {
		if n := len([]rune(line)); n > longest {
			longest = n
		}
	}
	return longest
}

// TestRenderWidthStaysInsideRendersContract: $COLUMNS wins when it parses and
// clears the minimum, and anything else falls back to the fixed default that
// makes a redirected `show` byte-identical on every machine.
func TestRenderWidthStaysInsideRendersContract(t *testing.T) {
	var buffer bytes.Buffer
	for _, testcase := range []struct {
		columns string
		want    int
	}{
		{"", render.DefaultWidth},
		{"not a number", render.DefaultWidth},
		{"0", render.DefaultWidth},
		{"10", render.DefaultWidth}, // at or below MinWidth is not a width
		{"64", 64},
		{"400", render.MaxWidth}, // capped, because prose past ~100 columns is harder to read
	} {
		t.Setenv("COLUMNS", testcase.columns)
		if testcase.columns == "" {
			os.Unsetenv("COLUMNS")
		}
		if got := renderWidth(&buffer); got != testcase.want {
			t.Errorf("renderWidth with COLUMNS=%q = %d, want %d", testcase.columns, got, testcase.want)
		}
	}
}

// TestPagerCommandHonoursAnExplicitlyEmptyPAGER: $PAGER wins, and an explicitly
// empty one DISABLES paging — which is how a caller turns paging off without a
// flag. An unset $PAGER is a different answer from an empty one.
func TestPagerCommandHonoursAnExplicitlyEmptyPAGER(t *testing.T) {
	os.Unsetenv("PAGER")
	if got := pagerCommand(); got != render.DefaultPagerCommand {
		t.Errorf("unset $PAGER = %q, want the default %q", got, render.DefaultPagerCommand)
	}
	t.Setenv("PAGER", "more")
	if got := pagerCommand(); got != "more" {
		t.Errorf("$PAGER=more = %q", got)
	}
	t.Setenv("PAGER", "   ")
	if got := pagerCommand(); got != "" {
		t.Errorf("an explicitly blank $PAGER = %q, want \"\" (paging off)", got)
	}
}

// TestConsoleOffATerminalHasNoTTYAndNoPager: a pipe or a test buffer gets no
// TTY and no pager, which is the single fact every byte-exact test in this
// package rests on.
func TestConsoleOffATerminalHasNoTTYAndNoPager(t *testing.T) {
	t.Setenv("PAGER", "less -FRX")
	var out, errOut bytes.Buffer
	console := newConsole(&out, &errOut)
	if console.TTY {
		t.Error("a bytes.Buffer was taken for a terminal")
	}
	if console.Pager != nil {
		t.Error("a non-terminal writer was given a pager")
	}
	if console.Width != render.DefaultWidth {
		t.Errorf("width off a terminal = %d, want the fixed default %d", console.Width, render.DefaultWidth)
	}
	// A closed pipe is a *os.File and still not a terminal.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if newConsole(writer, &errOut).TTY {
		t.Error("a pipe was taken for a terminal")
	}
}

// TestAmbiguousOriginIsReportedNeverGuessed: two projects claiming one origin
// is the case the resolution ladder refuses to resolve. Registering the second
// is allowed — the store does not police it — so the refusal has to happen when
// a command tries to route by it, and it has to name a way out.
func TestAmbiguousOriginIsReportedNeverGuessed(t *testing.T) {
	db, _ := newStore(t)
	repo := t.TempDir()
	gitRun(t, "", "init", "-q", repo)
	gitRun(t, repo, "remote", "add", "origin", "https://github.com/tedkulp/contested.git")

	mustRun(t, db, repo, "project", "add", "--slug", "first", "--remote", "https://github.com/tedkulp/contested.git")
	mustRun(t, db, repo, "project", "add", "--slug", "second", "--remote", "git@github.com:tedkulp/contested.git")

	_, _, err := RunForTest([]string{"list"}, db, repo)
	if err == nil {
		t.Fatal("a contested origin resolved to one project instead of being reported")
	}
	if ExitCodeFor(err) != 5 {
		t.Fatalf("a contested origin exit = %d, want 5 (a conflict)", ExitCodeFor(err))
	}
	message := err.Error()
	if !strings.Contains(message, "claimed by 2 projects") || !strings.Contains(message, "-P") {
		t.Fatalf("the refusal does not say what is wrong or how to get past it: %v", err)
	}
	// -P is the way out it names, and it works.
	if _, _, code := run(t, db, repo, "list", "-P", "first"); code != 0 {
		t.Error("the -P the refusal recommends does not resolve")
	}
}
