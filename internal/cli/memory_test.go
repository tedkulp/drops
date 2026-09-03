package cli

import (
	"strings"
	"testing"
)

// TestRememberFilesUnderTheResolvedProject: a memory write lands where an issue
// would, by ordinary scoping, and --global assigns the reserved global project
// instead. The two are mutually exclusive, because a memory has exactly one
// project and a contradiction cannot be resolved by guessing.
func TestRememberFilesUnderTheResolvedProject(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "beacon")

	scoped := mustRun(t, db, cwd, "remember", "-P", "beacon", "beacon keeps its own key")
	global := mustRun(t, db, cwd, "remember", "--global", "a workstation fact")
	loose := mustRun(t, db, cwd, "remember", "an unscoped note")

	if !strings.HasPrefix(scoped, "mem-") {
		t.Fatalf("remember printed %q, want a mem- prefixed id", scoped)
	}
	beaconKey := projectKeyOf(t, db, cwd, "beacon")
	globalKey := projectKeyOf(t, db, cwd, "global")
	inboxKey := projectKeyOf(t, db, cwd, "inbox")

	for id, want := range map[string]string{scoped: beaconKey, global: globalKey, loose: inboxKey} {
		got := decodeOne[map[string]any](t, mustRun(t, db, cwd, "memory", "show", id, "--json"))["project_key"]
		if got != want {
			t.Errorf("memory %s landed in %v, want %v", id, got, want)
		}
	}

	// A title is derived from the body when none is passed, and --title
	// overrides it.
	derived := decodeOne[map[string]any](t, mustRun(t, db, cwd, "memory", "show", global, "--json"))
	if derived["title"] != "a workstation fact" {
		t.Errorf("derived title = %v", derived["title"])
	}
	titled := mustRun(t, db, cwd, "remember", "--global", "--title", "The Title", "--source", "wayfinder", "the body")
	explicit := decodeOne[map[string]any](t, mustRun(t, db, cwd, "memory", "show", titled, "--json"))
	if explicit["title"] != "The Title" || explicit["provenance"] != "wayfinder" {
		t.Errorf("--title/--source did not land: %#v", explicit)
	}

	if _, _, code := run(t, db, cwd, "remember", "--global", "-P", "beacon", "which is it"); code != 2 {
		t.Error("--global together with -P was not refused at 2")
	}
}

func projectKeyOf(t *testing.T, db, cwd, slug string) string {
	t.Helper()
	for _, p := range decodeMany[map[string]any](t, mustRun(t, db, cwd, "project", "list", "--json")) {
		if p["slug"] == slug {
			key, _ := p["project_key"].(string)
			return key
		}
	}
	t.Fatalf("no project %q", slug)
	return ""
}

// TestMemoriesReadSpansEveryProject is the inverse of the issue verbs, and the
// point of the whole subsystem: a memory bound to one repo is still findable
// from another. -P narrows.
func TestMemoriesReadSpansEveryProject(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "beacon")
	mustRun(t, db, cwd, "project", "add", "--slug", "gitops")
	here := mustRun(t, db, cwd, "remember", "-P", "beacon", "a beacon fact")
	there := mustRun(t, db, cwd, "remember", "-P", "gitops", "a gitops fact")

	// From a directory that resolves to NO project, both are still found —
	// which is exactly where a scanning verb would print its advisory and
	// return nothing.
	every := mustRun(t, db, cwd, "memories")
	if !strings.Contains(every, here) || !strings.Contains(every, there) {
		t.Fatalf("memories did not span every project:\n%s", every)
	}
	narrowed := mustRun(t, db, cwd, "memories", "-P", "beacon")
	if !strings.Contains(narrowed, here) || strings.Contains(narrowed, there) {
		t.Fatalf("-P did not narrow the read:\n%s", narrowed)
	}
	if _, _, code := run(t, db, cwd, "memories", "-P", "nosuch"); code != 4 {
		t.Error("an unknown -P was not a not-found refusal")
	}

	// A query searches rather than lists, and --limit caps.
	found := mustRun(t, db, cwd, "memories", "beacon")
	if !strings.Contains(found, here) || strings.Contains(found, there) {
		t.Fatalf("a query did not search:\n%s", found)
	}
	if got := mustRun(t, db, cwd, "memories", "--limit", "1"); len(strings.Split(got, "\n")) != 1 {
		t.Fatalf("--limit 1 returned:\n%s", got)
	}

	// The row is the shared glyph, the id, and the title.
	row, _, _ := run(t, db, cwd, "memories", "-P", "beacon")
	if row != "○ "+here+"  a beacon fact\n" {
		t.Fatalf("memory row = %q", row)
	}
}

// TestMemoryShowPrintsTitleLineThenBody: a header line naming the memory and
// its state, then the body verbatim. It reaches a retired or tombstoned memory
// that the listings hide, which is what "by exact id" is for.
func TestMemoryShowPrintsTitleLineThenBody(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "remember", "--global", "--source", "wayfinder", "the body of the note")

	out, _, code := run(t, db, cwd, "memory", "show", id)
	if code != 0 {
		t.Fatalf("memory show exit = %d", code)
	}
	if out != id+" · the body of the note · from wayfinder\nthe body of the note\n" {
		t.Fatalf("memory show = %q", out)
	}

	replacement := mustRun(t, db, cwd, "supersede", id, "the corrected note")
	if got := mustRun(t, db, cwd, "memory", "show", id); !strings.Contains(got, "superseded by "+replacement) {
		t.Fatalf("show of a retired memory does not say what replaced it: %q", got)
	}
	mustRun(t, db, cwd, "forget", replacement)
	if got := mustRun(t, db, cwd, "memory", "show", replacement); !strings.Contains(got, "tombstoned") {
		t.Fatalf("show of a tombstoned memory does not say so: %q", got)
	}
	if _, _, code := run(t, db, cwd, "memory", "show", "mem-zzzzz"); code != 4 {
		t.Error("an unknown memory id was not a not-found refusal")
	}
}

// TestSupersedeRetiresTheOldMemoryAndInheritsItsProject: one transaction that
// mints the replacement, links the old one to it and retires it. The
// replacement ALWAYS inherits the old memory's project, because a supersession
// chain lives in one project and the schema enforces it.
func TestSupersedeRetiresTheOldMemoryAndInheritsItsProject(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "beacon")
	old := mustRun(t, db, cwd, "remember", "-P", "beacon", "the original claim")

	replacement := mustRun(t, db, cwd, "supersede", old, "the corrected claim")
	if replacement == old {
		t.Fatal("supersede returned the old id")
	}

	beacon := projectKeyOf(t, db, cwd, "beacon")
	got := decodeOne[map[string]any](t, mustRun(t, db, cwd, "memory", "show", replacement, "--json"))
	if got["project_key"] != beacon {
		t.Fatalf("the replacement landed in %v, want the old memory's project %v", got["project_key"], beacon)
	}

	// memories shows only the current end of the chain; -a reveals the
	// retired link and says what replaced it.
	current := mustRun(t, db, cwd, "memories")
	if strings.Contains(current, old) || !strings.Contains(current, replacement) {
		t.Fatalf("the default listing is not the current end of the chain:\n%s", current)
	}
	all := mustRun(t, db, cwd, "memories", "-a")
	if !strings.Contains(all, old+"  the original claim · superseded by "+replacement) {
		t.Fatalf("-a does not reveal the retired link and its replacement:\n%s", all)
	}

	// A -P naming anywhere else is refused rather than accepted and thrown
	// away, and there is no --global at all: core overrides the project on
	// this path, so either flag could only ever have reported a move it did
	// not make.
	mustRun(t, db, cwd, "project", "add", "--slug", "elsewhere")
	fresh := mustRun(t, db, cwd, "remember", "-P", "beacon", "another claim")
	if _, _, code := run(t, db, cwd, "supersede", fresh, "-P", "elsewhere", "moved?"); code != 2 {
		t.Error("supersede -P naming another project was not refused at 2")
	}
	if _, _, code := run(t, db, cwd, "supersede", fresh, "-P", "beacon", "agreeing"); code != 0 {
		t.Error("supersede -P naming the memory's own project was refused")
	}
	if _, _, code := run(t, db, cwd, "supersede", old, "--global", "gone"); code != 2 {
		t.Error("supersede still carries a --global that core would override")
	}
	if _, _, code := run(t, db, cwd, "supersede", old, "a third claim"); code != 5 {
		t.Error("superseding an already-retired memory was not a conflict at 5")
	}
}

// TestMemoryEditRefusesToSplitASupersessionChain is the schema rule surfaced as
// a refusal rather than a raw foreign-key error: correct the text in place, or
// remember a fresh memory in the destination.
func TestMemoryEditRefusesToSplitASupersessionChain(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "beacon")
	mustRun(t, db, cwd, "project", "add", "--slug", "gitops")
	old := mustRun(t, db, cwd, "remember", "-P", "beacon", "the original claim")
	replacement := mustRun(t, db, cwd, "supersede", old, "the corrected claim")

	for _, link := range []string{old, replacement} {
		for _, scope := range [][]string{{"-P", "gitops"}, {"--global"}} {
			args := append([]string{"memory", "edit", link}, scope...)
			_, _, code := run(t, db, cwd, args...)
			if code != 2 {
				t.Errorf("%v exit = %d, want 2 (a chain lives in one project)", args, code)
			}
		}
	}

	// The content edits still work on a chain, and only the flags passed change.
	mustRun(t, db, cwd, "memory", "edit", replacement, "--title", "a better title")
	got := decodeOne[map[string]any](t, mustRun(t, db, cwd, "memory", "show", replacement, "--json"))
	if got["title"] != "a better title" || got["body"] != "the corrected claim" {
		t.Fatalf("memory edit --title changed something else: %#v", got)
	}

	// And a free memory does move, so the refusal is about the chain and
	// not about the flag.
	free := mustRun(t, db, cwd, "remember", "-P", "beacon", "an unchained note")
	mustRun(t, db, cwd, "memory", "edit", free, "-P", "gitops")
	moved := decodeOne[map[string]any](t, mustRun(t, db, cwd, "memory", "show", free, "--json"))
	if moved["project_key"] != projectKeyOf(t, db, cwd, "gitops") {
		t.Fatalf("an unchained memory did not move: %#v", moved)
	}
}

// TestForgetTombstonesAndHidesTheMemory: the row is never deleted, because
// something may reference it, and --deleted brings it back marked ⊘.
func TestForgetTombstonesAndHidesTheMemory(t *testing.T) {
	db, cwd := newStore(t)
	first := mustRun(t, db, cwd, "remember", "--global", "the first note")
	second := mustRun(t, db, cwd, "remember", "--global", "the second note")
	kept := mustRun(t, db, cwd, "remember", "--global", "the kept note")

	out := mustRun(t, db, cwd, "forget", first, second)
	if out != "forgot "+first+"\nforgot "+second {
		t.Fatalf("forget printed %q", out)
	}

	live := mustRun(t, db, cwd, "memories")
	if strings.Contains(live, first) || strings.Contains(live, second) {
		t.Fatalf("a tombstoned memory is still in the default listing:\n%s", live)
	}
	if !strings.Contains(live, kept) {
		t.Fatalf("forget took a memory it was not given:\n%s", live)
	}

	deleted := mustRun(t, db, cwd, "memories", "--deleted")
	if !strings.Contains(deleted, "⊘ "+first) || !strings.Contains(deleted, "⊘ "+second) {
		t.Fatalf("--deleted does not show the tombstones marked ⊘:\n%s", deleted)
	}
	if strings.Contains(deleted, kept) {
		t.Fatalf("--deleted showed a live memory:\n%s", deleted)
	}
	// The row survives, so an exact id still reaches it.
	if _, _, code := run(t, db, cwd, "memory", "show", first); code != 0 {
		t.Error("forget deleted the row rather than tombstoning it")
	}
	if _, _, code := run(t, db, cwd, "forget", "mem-zzzzz"); code != 4 {
		t.Error("forgetting an unknown memory was not a not-found refusal")
	}
}

// TestATombstoneOutranksASupersession: a memory can be both retired and
// removed, and the state it reports has to be the removal. A retired memory
// reading as merely superseded invites someone to go and read the replacement
// of something that is not there.
func TestATombstoneOutranksASupersession(t *testing.T) {
	db, cwd := newStore(t)
	old := mustRun(t, db, cwd, "remember", "--global", "the original claim")
	replacement := mustRun(t, db, cwd, "supersede", old, "the corrected claim")
	mustRun(t, db, cwd, "forget", old)

	row := mustRun(t, db, cwd, "memories", "--deleted")
	if !strings.HasPrefix(row, "⊘ "+old) {
		t.Fatalf("a memory that is both retired and removed does not lead with ⊘: %q", row)
	}
	if !strings.HasSuffix(row, "· tombstoned") {
		t.Fatalf("it reports %q, want tombstoned rather than the supersession under it", row)
	}
	if strings.Contains(row, replacement) {
		t.Fatalf("it still points at its replacement: %q", row)
	}
	if page := mustRun(t, db, cwd, "memory", "show", old); !strings.HasSuffix(strings.Split(page, "\n")[0], "· tombstoned") {
		t.Fatalf("memory show says %q", page)
	}
}
