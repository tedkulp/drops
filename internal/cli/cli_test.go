package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/sync"
)

// run executes the CLI once against one store and returns stdout, stderr, and
// the mapped exit code. Callers share one dbPath across invocations so writes
// from an earlier call are visible to a later one.
func run(t *testing.T, dbPath, cwd string, args ...string) (string, string, int) {
	t.Helper()
	out, errOut, err := RunForTest(args, dbPath, cwd)
	return out, errOut, ExitCodeFor(err)
}

func TestExitCodeForSentinels(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{fmt.Errorf("%w: bad", model.ErrInvalid), 2},
		{fmt.Errorf("%w: gone", model.ErrNotFound), 4},
		{fmt.Errorf("%w: clash", model.ErrConflict), 5},
		{fmt.Errorf("%w: held", sync.ErrSyncLocked), 10},
		{fmt.Errorf("plain failure"), 1},
	}
	for _, c := range cases {
		if got := ExitCodeFor(c.err); got != c.want {
			t.Errorf("ExitCodeFor(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestParserMisuseExitsTwo(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	_, _, code := run(t, db, t.TempDir(), "list", "--bogus")
	if code != 2 {
		t.Fatalf("parser misuse exit = %d, want 2", code)
	}
}

func TestInvalidArgsExitsTwo(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	_, _, code := run(t, db, t.TempDir(), "create")
	if code != 2 {
		t.Fatalf("create with no title exit = %d, want 2", code)
	}
}

func TestNotFoundExitsFour(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	_, _, code := run(t, db, t.TempDir(), "show", "zzzzz")
	if code != 4 {
		t.Fatalf("show unknown id exit = %d, want 4", code)
	}
}

func TestMalformedInvocationDoesNotMutate(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()
	_, _, code := run(t, db, cwd, "create", "x", "-t", "not-a-type")
	if code != 2 {
		t.Fatalf("create with bad type exit = %d, want 2", code)
	}
	out, _, _ := run(t, db, cwd, "count", "--all-projects", "--json")
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("count --json did not unmarshal: %v (%q)", err, out)
	}
	if payload.Count != 0 {
		t.Fatalf("store mutated by refused create: count = %d, want 0", payload.Count)
	}
}

func TestUpdateClearAssignee(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()
	out, _, _ := run(t, db, cwd, "q", "--inbox", "who")
	id := strings.TrimSpace(out)
	if id == "" {
		t.Fatalf("q produced no id (out=%q)", out)
	}
	run(t, db, cwd, "update", id, "-A", "alice")

	out, _, _ = run(t, db, cwd, "show", id, "--json")
	var set struct {
		Assignee *string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(out), &set); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if set.Assignee == nil || *set.Assignee != "alice" {
		t.Fatalf("assignee after -A alice = %v, want alice", set.Assignee)
	}

	run(t, db, cwd, "update", id, "-A", "")
	out, _, _ = run(t, db, cwd, "show", id, "--json")
	var cleared struct {
		Assignee *string `json:"assignee"`
	}
	if err := json.Unmarshal([]byte(out), &cleared); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if cleared.Assignee != nil {
		t.Fatalf("assignee after -A \"\" = %v, want null (cleared)", *cleared.Assignee)
	}
}

func TestReadyRowByteExact(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()
	out, _, code := run(t, db, cwd, "q", "--inbox", "hello world")
	if code != 0 {
		t.Fatalf("q exit = %d", code)
	}
	id := strings.TrimSpace(out)
	out, _, code = run(t, db, cwd, "ready", "--inbox")
	if code != 0 {
		t.Fatalf("ready exit = %d", code)
	}
	want := fmt.Sprintf("○ %s P2 task     hello world\n", id)
	if out != want {
		t.Fatalf("ready row = %q, want %q", out, want)
	}
}

func TestShowJSONShape(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()
	out, _, _ := run(t, db, cwd, "q", "--inbox", "shape")
	id := strings.TrimSpace(out)
	out, _, _ = run(t, db, cwd, "show", id, "--json")
	var view map[string]any
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("show --json not an object: %v (%q)", err, out)
	}
	for _, key := range []string{"parent", "blockers", "blocking", "children", "comments"} {
		if _, ok := view[key]; !ok {
			t.Fatalf("show --json missing key %q (empty relations must be present)", key)
		}
	}
	if view["parent"] != nil {
		t.Fatalf("parent = %v, want null when none", view["parent"])
	}
	for _, key := range []string{"blockers", "blocking", "children", "comments"} {
		list, ok := view[key].([]any)
		if !ok || list == nil {
			t.Fatalf("%s = %v, want []", key, view[key])
		}
	}
}

func TestCreateParentConflict(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()
	out, _, _ := run(t, db, cwd, "q", "--inbox", "parent issue")
	parent := strings.TrimSpace(out)
	_, _, code := run(t, db, cwd, "create", "child", "--parent", parent, "-P", "global")
	if code != 2 {
		t.Fatalf("create --parent with conflicting -P exit = %d, want 2", code)
	}
}

func TestReadyExcludesClosed(t *testing.T) {
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()
	out, _, _ := run(t, db, cwd, "q", "--inbox", "one")
	id := strings.TrimSpace(out)
	run(t, db, cwd, "close", id)
	out, _, _ = run(t, db, cwd, "list", "--inbox")
	if strings.Contains(out, id) {
		t.Fatalf("closed issue %s present in default list:\n%s", id, out)
	}
	out, _, _ = run(t, db, cwd, "list", "--inbox", "-a")
	if !strings.Contains(out, id) {
		t.Fatalf("closed issue %s absent from -a list:\n%s", id, out)
	}
}

func TestUnknownCommandExitsTwo(t *testing.T) {
	// The clause: a mistyped verb is "you called it wrong" (2), never a
	// crash (1) and never success (0). Cobra's own answers are both wrong —
	// legacyArgs rejects an unknown root command with a bare error, which
	// lands on 1, and a group with no Run prints its help and exits 0, so
	// `drops comment edit <id> "..."` reported success having written
	// nothing.
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()
	for _, testcase := range []struct {
		name string
		args []string
	}{
		{"unknown root verb", []string{"nosuchverb"}},
		{"unknown subcommand", []string{"comment", "edit", "zzzzz", "body"}},
		{"unknown dep subcommand", []string{"dep", "frobnicate", "zzzzz"}},
		{"unknown memory subcommand", []string{"memory", "forget", "mem-zzzzz"}},
		{"unknown project subcommand", []string{"project", "delete", "x"}},
		{"unknown label subcommand", []string{"label", "count"}},
		{"unknown replica subcommand", []string{"replica", "show"}},
		{"unknown config subcommand", []string{"config", "set", "x"}},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			_, _, code := run(t, db, cwd, testcase.args...)
			if code != 2 {
				t.Fatalf("%v exit = %d, want 2", testcase.args, code)
			}
		})
	}
}

func TestBareGroupPrintsHelpAndSucceeds(t *testing.T) {
	// The other side of the same branch: a group with no positional is a
	// request for its help, not a misuse.
	db := filepath.Join(t.TempDir(), "drops.db")
	out, _, code := run(t, db, t.TempDir(), "dep")
	if code != 0 {
		t.Fatalf("bare group exit = %d, want 0", code)
	}
	if !strings.Contains(out, "Available Commands:") {
		t.Fatalf("bare group printed no help: %q", out)
	}
}

func TestSearchHonoursAllFlag(t *testing.T) {
	// The clause: -a means the same on search as on every other scanning
	// verb. Search answered from every live status regardless, so the flag
	// an agent passes to reach a closed issue was inert and the default was
	// wider than list's — a closed issue appeared in results that claimed to
	// be open work.
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()

	openOut, _, _ := run(t, db, cwd, "q", "--inbox", "cobra stays for now")
	openID := strings.TrimSpace(openOut)
	closedOut, _, _ := run(t, db, cwd, "q", "--inbox", "cobra was reconsidered")
	closedID := strings.TrimSpace(closedOut)
	if _, _, code := run(t, db, cwd, "close", closedID, "--reason", "settled"); code != 0 {
		t.Fatalf("close %s failed", closedID)
	}

	bare, _, code := run(t, db, cwd, "search", "cobra", "--inbox")
	if code != 0 {
		t.Fatalf("search exit = %d, want 0", code)
	}
	if !strings.Contains(bare, openID) {
		t.Errorf("search dropped the open issue %s: %q", openID, bare)
	}
	if strings.Contains(bare, closedID) {
		t.Errorf("search without -a returned the closed issue %s: %q", closedID, bare)
	}

	all, _, _ := run(t, db, cwd, "search", "cobra", "--inbox", "-a")
	if !strings.Contains(all, closedID) {
		t.Errorf("search -a missed the closed issue %s: %q", closedID, all)
	}
	if !strings.Contains(all, openID) {
		t.Errorf("search -a dropped the open issue %s: %q", openID, all)
	}
}

func TestLabelListCountsAreProjectScoped(t *testing.T) {
	// The clause: `label list` with no id counts over the scope every other
	// read uses. The old build ignored -P and the ambient project and
	// answered store-wide, so two projects printed the same table byte for
	// byte with nothing to tell them apart (i-wnkh7). The rewrite does not
	// inherit it.
	db := filepath.Join(t.TempDir(), "drops.db")
	cwd := t.TempDir()

	if _, _, code := run(t, db, cwd, "project", "add", "--slug", "alpha"); code != 0 {
		t.Fatal("project add alpha failed")
	}
	if _, _, code := run(t, db, cwd, "project", "add", "--slug", "beta"); code != 0 {
		t.Fatal("project add beta failed")
	}
	if _, _, code := run(t, db, cwd, "create", "in alpha", "-P", "alpha", "-l", "here"); code != 0 {
		t.Fatal("create in alpha failed")
	}

	alpha, _, code := run(t, db, cwd, "label", "list", "-P", "alpha")
	if code != 0 {
		t.Fatalf("label list -P alpha exit = %d, want 0", code)
	}
	if !strings.Contains(alpha, "here") {
		t.Errorf("label list -P alpha missed its own label: %q", alpha)
	}

	beta, _, _ := run(t, db, cwd, "label", "list", "-P", "beta")
	if strings.Contains(beta, "here") {
		t.Errorf("label list -P beta counted alpha's label: %q", beta)
	}

	every, _, _ := run(t, db, cwd, "label", "list", "--all-projects")
	if !strings.Contains(every, "here") {
		t.Errorf("label list --all-projects missed the label: %q", every)
	}
}
