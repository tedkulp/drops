package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectAddRegistersASlug: the explicit registration path, as against the
// implicit one a write from an unregistered repository takes.
func TestProjectAddRegistersASlug(t *testing.T) {
	db, cwd := newStore(t)
	if got := mustRun(t, db, cwd, "project", "add", "--slug", "beacon"); got != "added beacon" {
		t.Fatalf("project add printed %q", got)
	}
	if out := mustRun(t, db, cwd, "project", "list"); !strings.Contains(out, "beacon") {
		t.Fatalf("the new project is not listed:\n%s", out)
	}
	// It is a real scope immediately: an issue can be filed into it by slug.
	id := mustRun(t, db, cwd, "create", "first", "-P", "beacon")
	if out := mustRun(t, db, cwd, "list", "-P", "beacon"); !strings.Contains(out, id) {
		t.Fatalf("-P beacon does not find its own issue:\n%s", out)
	}

	// --slug is required, and a name already taken is a conflict rather
	// than a second project answering to one slug.
	_, _, err := RunForTest([]string{"project", "add"}, db, cwd)
	if ExitCodeFor(err) != 2 {
		t.Errorf("project add with no --slug exit = %d, want 2", ExitCodeFor(err))
	} else if !strings.Contains(err.Error(), "--slug") {
		t.Errorf("the refusal does not name the missing flag: %v", err)
	}
	if _, _, code := run(t, db, cwd, "project", "add", "--slug", "beacon"); code == 0 {
		t.Error("project add accepted a slug that was already taken")
	}
}

// TestProjectAddBindsARepositoryPath is the promise the doc leads with — run a
// command from inside the repo the work belongs to and the scoping is automatic
// — reached by the explicit registration path. The path is canonicalized on the
// way in, so the binding a symlinked checkout produces byte-matches the one
// stored, which is the only reason the two ever reconcile for one directory.
func TestProjectAddBindsARepositoryPath(t *testing.T) {
	db, _ := newStore(t)
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	deep := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "checkout")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}

	// Registered through the SYMLINK, so the stored binding can only match a
	// checkout reached by its real path if the path was canonicalized on the
	// way in.
	mustRun(t, db, repo, "project", "add", "--slug", "bound", "--repo-path", link)
	for name, cwd := range map[string]string{
		"the repository root":  repo,
		"a subdirectory":       deep,
		"a symlinked checkout": link,
	} {
		out, errOut, code := run(t, db, cwd, "config", "show")
		if code != 0 || !strings.Contains(out, "project.slug=bound") {
			t.Errorf("from %s: config show = %q (stderr %q), want project.slug=bound", name, out, errOut)
		}
	}
	// And a write from there lands in that project without -P.
	id := mustRun(t, db, deep, "q", "an issue from inside the repo")
	if out := mustRun(t, db, repo, "list", "-P", "bound"); !strings.Contains(out, id) {
		t.Fatalf("the issue did not land in the bound project:\n%s", out)
	}
}

// TestProjectListMapsKeyToSlug: the doc points a parser here to turn the opaque
// project_key that travels in JSON into the slug a human reads.
func TestProjectListMapsKeyToSlug(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "beacon")
	id := mustRun(t, db, cwd, "create", "an issue", "-P", "beacon")

	issue := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))
	key, _ := issue["project_key"].(string)
	if key == "" || key == "beacon" {
		t.Fatalf("an issue's project in --json = %q, want an opaque key, never a slug", key)
	}

	projects := decodeMany[map[string]any](t, mustRun(t, db, cwd, "project", "list", "--json"))
	found := ""
	for _, p := range projects {
		if p["project_key"] == key {
			found, _ = p["slug"].(string)
		}
	}
	if found != "beacon" {
		t.Fatalf("project list --json does not map %s back to beacon: %#v", key, projects)
	}

	// The text form is the slug alone, one per line, and the two reserved
	// projects are always there.
	out := mustRun(t, db, cwd, "project", "list")
	for _, want := range []string{"beacon", "inbox", "global"} {
		if !strings.Contains(out, want) {
			t.Errorf("project list is missing %q:\n%s", want, out)
		}
	}
}

// TestProjectRenameKeepsTheProjectKey is the point of having a key at all:
// slugs are renameable metadata, keys are what sync agrees on, so a rename must
// not disturb a single stored reference.
func TestProjectRenameKeepsTheProjectKey(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "before")
	id := mustRun(t, db, cwd, "create", "an issue", "-P", "before")
	was := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))["project_key"]

	if got := mustRun(t, db, cwd, "project", "rename", "before", "after"); got != "renamed after" {
		t.Fatalf("project rename printed %q", got)
	}
	now := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))["project_key"]
	if now != was {
		t.Fatalf("rename changed the project key from %v to %v", was, now)
	}
	if out := mustRun(t, db, cwd, "list", "-P", "after"); !strings.Contains(out, id) {
		t.Fatalf("the new slug does not find the issue:\n%s", out)
	}
	if _, _, code := run(t, db, cwd, "list", "-P", "before"); code != 4 {
		t.Error("the old slug still resolves after a rename")
	}
	if _, _, code := run(t, db, cwd, "project", "rename", "nosuch", "whatever"); code != 4 {
		t.Error("renaming an unknown project was not a not-found refusal")
	}
}

// TestProjectArchiveHidesItFromListAndRefusesNewIssues pins what archiving
// actually does, which is narrower than the verb's help used to claim: the
// project leaves `project list`, and it takes no new work. Its existing issues
// stay readable by -P and stay inside --all-projects, because --all-projects
// means every project.
func TestProjectArchiveHidesItFromListAndRefusesNewIssues(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "retired")
	id := mustRun(t, db, cwd, "create", "old work", "-P", "retired")

	// Destructive, so a non-interactive session has to say --force.
	if _, _, code := run(t, db, cwd, "project", "archive", "retired"); code != 2 {
		t.Error("project archive went ahead without --force from a non-interactive session")
	}
	if got := mustRun(t, db, cwd, "project", "archive", "retired", "--force"); got != "archived retired" {
		t.Fatalf("project archive printed %q", got)
	}

	if out := mustRun(t, db, cwd, "project", "list"); strings.Contains(out, "retired") {
		t.Errorf("an archived project is still in project list:\n%s", out)
	}
	if out := mustRun(t, db, cwd, "project", "list", "--archived"); !strings.Contains(out, "retired") {
		t.Errorf("--archived does not bring it back:\n%s", out)
	}
	if _, _, code := run(t, db, cwd, "create", "new work", "-P", "retired"); code != 2 {
		t.Error("an archived project accepted a new issue")
	}
	if _, _, code := run(t, db, cwd, "remember", "-P", "retired", "a note"); code != 2 {
		t.Error("an archived project accepted a new memory")
	}
	if out := mustRun(t, db, cwd, "list", "-P", "retired"); !strings.Contains(out, id) {
		t.Errorf("archiving hid an existing issue from -P:\n%s", out)
	}
	if out := mustRun(t, db, cwd, "list", "--all-projects"); !strings.Contains(out, id) {
		t.Errorf("archiving hid an existing issue from --all-projects:\n%s", out)
	}
	// The reserved projects are not retirable: everything falls back to them.
	for _, reserved := range []string{"inbox", "global"} {
		if _, _, code := run(t, db, cwd, "project", "archive", reserved, "--force"); code == 0 {
			t.Errorf("archived the reserved project %q", reserved)
		}
	}
}
