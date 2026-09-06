package cli

import (
	"strings"
	"testing"
)

// TestCompletionGeneratesInstalledShellScriptsWithoutAStore is the public
// completion verb's contract: drops supports the three shells this Unix-only
// program can exercise here, and script generation cannot depend on a store.
func TestCompletionGeneratesInstalledShellScriptsWithoutAStore(t *testing.T) {
	for _, testcase := range []struct {
		shell  string
		marker string
	}{
		{shell: "bash", marker: "__start_drops"},
		{shell: "zsh", marker: "#compdef drops"},
		{shell: "fish", marker: "complete -c drops"},
	} {
		t.Run(testcase.shell, func(t *testing.T) {
			out, errOut, code := run(t,
				"/dev/null/no-completion-store/drops.db", t.TempDir(),
				"completion", testcase.shell,
			)
			if code != 0 {
				t.Fatalf("completion %s exit = %d, want 0 (stderr: %s)", testcase.shell, code, errOut)
			}
			if !strings.Contains(out, testcase.marker) {
				t.Fatalf("completion %s did not generate its shell script", testcase.shell)
			}
		})
	}

	_, _, code := run(t, "ignored.db", t.TempDir(), "completion", "powershell")
	if code != 2 {
		t.Fatalf("unsupported completion shell exit = %d, want 2", code)
	}
}

// TestIssueCompletionIsAProjectScopedLivePicker proves the completion is the
// useful form the old decision said was missing: ids carry titles, default to
// the current project's open and in_progress work, and widen only through the
// same -a and --all-projects flags as list.
func TestIssueCompletionIsAProjectScopedLivePicker(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "alpha")
	mustRun(t, db, cwd, "project", "add", "--slug", "beta")

	open := mustRun(t, db, cwd, "create", "alpha open", "-P", "alpha")
	started := mustRun(t, db, cwd, "create", "alpha started", "-P", "alpha")
	mustRun(t, db, cwd, "update", started, "--status", "in_progress")
	closed := mustRun(t, db, cwd, "create", "alpha closed", "-P", "alpha")
	mustRun(t, db, cwd, "close", closed)
	other := mustRun(t, db, cwd, "create", "beta open", "-P", "beta")

	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "show", "-P", "alpha", ""},
		open+"\talpha open", started+"\talpha started",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "show", "-P", "alpha", "-a", ""},
		open+"\talpha open", started+"\talpha started", closed+"\talpha closed",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "show", "--all-projects", ""},
		open+"\talpha open", started+"\talpha started", other+"\tbeta open",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "show", "-P", "alpha", open},
		open+"\talpha open",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "reopen", "-P", "alpha", ""},
		closed+"\talpha closed",
	)
}

// TestStaticFlagCompletionUsesTheClosedVocabularies keeps completion on the
// same seven issue types, three stored statuses, list's "all" widening value,
// and five priorities that the commands accept.
func TestStaticFlagCompletionUsesTheClosedVocabularies(t *testing.T) {
	db, cwd := newStore(t)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "create", "--type", ""},
		"task", "bug", "feature", "epic", "chore", "research", "decision",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "update", "--status", ""},
		"open", "in_progress", "closed",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "list", "--status", ""},
		"open", "in_progress", "closed", "all",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "create", "--priority", ""},
		"0", "1", "2", "3", "4",
	)
}

// TestProjectAndLabelCompletionUseTheStore makes the other dynamic values
// useful without accepting less than their commands do: -P can address an
// archived project for historical reads, while move --to cannot; labels are
// the existing names in the selected scope.
func TestProjectAndLabelCompletionUseTheStore(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "alpha")
	mustRun(t, db, cwd, "project", "add", "--slug", "retired")
	mustRun(t, db, cwd, "create", "label source", "-P", "alpha", "-l", "wayfinder:map")
	mustRun(t, db, cwd, "project", "archive", "retired", "--force")

	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "list", "--project", "ret"},
		"retired",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "move", "--to", "ret"},
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "list", "-P", "alpha", "--label", "way"},
		"wayfinder:map",
	)
}

func assertCompletionSet(t *testing.T, db, cwd string, args []string, want ...string) {
	t.Helper()
	out, errOut, code := run(t, db, cwd, args...)
	if code != 0 {
		t.Fatalf("%v exit = %d, want 0 (stderr: %s)", args, code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 || lines[len(lines)-1] != ":4" {
		t.Fatalf("%v directive = %q, want :4 (no file completion)", args, lines)
	}
	lines = lines[:len(lines)-1]
	if len(lines) != len(want) {
		t.Fatalf("%v completions = %q, want %q", args, lines, want)
	}
	got := make(map[string]bool, len(lines))
	for _, line := range lines {
		got[line] = true
	}
	for _, completion := range want {
		if !got[completion] {
			t.Fatalf("%v completions = %q, missing %q", args, lines, completion)
		}
	}
}

// TestIDFlagCompletionIgnoresATypedTitle is 99c27: `create --parent <TAB>` is
// typed after the title, not before it, so a flag completion that consults the
// command's positional arity offers nothing in the order anyone actually types.
// Cobra hands a flag completion the positionals too, and they mean nothing to
// it.
func TestIDFlagCompletionIgnoresATypedTitle(t *testing.T) {
	db, cwd := newStore(t)
	first := mustRun(t, db, cwd, "create", "issue A", "--inbox")
	second := mustRun(t, db, cwd, "create", "issue B", "--inbox")

	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "create", "--inbox", "--parent", ""},
		first+"\tissue A", second+"\tissue B",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "create", "--inbox", "my title", "--parent", ""},
		first+"\tissue A", second+"\tissue B",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "list", "--inbox", "--parent", ""},
		first+"\tissue A", second+"\tissue B",
	)
}

// TestPositionalIDCompletionStopsAfterTheLastIDPosition is the duty the arity
// guard actually has, kept while 99c27 took it off the flag path: `comment add
// <id> <body>` completes ids for the id and nothing for the body, and the ids
// a command has already accepted are not offered a second time.
func TestPositionalIDCompletionStopsAfterTheLastIDPosition(t *testing.T) {
	db, cwd := newStore(t)
	first := mustRun(t, db, cwd, "create", "issue A", "--inbox")
	second := mustRun(t, db, cwd, "create", "issue B", "--inbox")

	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "comment", "add", "--inbox", ""},
		first+"\tissue A", second+"\tissue B",
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "comment", "add", "--inbox", first, ""},
	)
	assertCompletionSet(t, db, cwd,
		[]string{"__complete", "dep", "add", "--inbox", first, ""},
		second+"\tissue B",
	)
}
