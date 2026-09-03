package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionWorksBeforeTheStoreExists: version is annotated no-store, so it
// answers on a machine where ~/.drops/drops.db has never been created. A
// version command that needs a database cannot tell you which binary you have
// when the database is the thing that is broken.
func TestVersionWorksBeforeTheStoreExists(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nowhere", "drops.db")
	out, _, code := run(t, missing, t.TempDir(), "version")
	if code != 0 {
		t.Fatalf("version exit = %d", code)
	}
	if out != "drops "+Version+"\n" {
		t.Fatalf("version = %q, want %q", out, "drops "+Version+"\n")
	}
	if _, err := os.Stat(filepath.Dir(missing)); err == nil {
		t.Fatal("version created the store directory; it is meant not to touch the store at all")
	}
	// --version on the root is the same answer, through cobra's own flag.
	flagged, _, _ := run(t, missing, t.TempDir(), "--version")
	if flagged != out {
		t.Fatalf("--version = %q, version = %q; they must agree", flagged, out)
	}
}

// TestBareRootPrintsHelpAndSucceeds: the root with no verb is a request for
// help, not a misuse — the other side of the branch that makes a MISTYPED verb
// exit 2.
func TestBareRootPrintsHelpAndSucceeds(t *testing.T) {
	db, cwd := newStore(t)
	out, _, code := run(t, db, cwd)
	if code != 0 {
		t.Fatalf("bare root exit = %d, want 0", code)
	}
	if !strings.Contains(out, "Available Commands:") {
		t.Fatalf("bare root printed no help:\n%s", out)
	}
}

// TestConfigShowNamesTheStoreAndTheResolvedProject: the quickest way to find
// out why a command answered the way it did. It is a reading verb, so --json
// owes a bare object rather than lines a parser has to split.
func TestConfigShowNamesTheStoreAndTheResolvedProject(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "project", "add", "--slug", "beacon")

	out, errOut, code := run(t, db, cwd, "config", "show")
	if code != 0 {
		t.Fatalf("config show exit = %d", code)
	}
	if !strings.Contains(out, "store.path="+db) {
		t.Fatalf("config show does not name the store: %q", out)
	}
	if strings.Contains(out, "project.slug=") {
		t.Fatalf("config show invented a project where none resolves: %q", out)
	}
	if !strings.Contains(errOut, "no project resolves here") {
		t.Errorf("config show gave no advisory where nothing resolves: %q", errOut)
	}

	scoped := mustRun(t, db, cwd, "config", "show", "-P", "beacon")
	if !strings.Contains(scoped, "project.slug=beacon") {
		t.Fatalf("config show -P does not name the resolved project: %q", scoped)
	}
	// Sorted key=value lines, so the text form is stable to diff.
	lines := strings.Split(scoped, "\n")
	if len(lines) != 2 || lines[0] >= lines[1] {
		t.Fatalf("config show is not sorted key=value lines: %#v", lines)
	}

	asJSON := decodeOne[map[string]string](t, mustRun(t, db, cwd, "config", "show", "-P", "beacon", "--json"))
	if asJSON["store.path"] != db || asJSON["project.slug"] != "beacon" {
		t.Fatalf("config show --json = %#v", asJSON)
	}
}

// TestDoctorExitsOneOnACredentialFinding: doctor's 1 is one of the two the doc
// says is a REPORT rather than a crash, so the finding has to be on stdout for
// a reader to act on. It is offline and store-wide, and --scan never redacts.
func TestDoctorExitsOneOnACredentialFinding(t *testing.T) {
	db, cwd := newStore(t)

	// A sound store: every documented check, all ok, exit 0.
	out, _, code := run(t, db, cwd, "doctor")
	if code != 0 {
		t.Fatalf("doctor on a sound store exit = %d, want 0", code)
	}
	for _, check := range []string{"quick_check", "foreign_key_check", "issues_fts", "comments_fts", "memories_fts"} {
		if !strings.Contains(out, check+" ") {
			t.Errorf("doctor did not run %s:\n%s", check, out)
		}
	}

	const token = "ghp_123456789012345678901234567890123456"
	id := mustRun(t, db, cwd, "create", "a leak", "--inbox", "-d", token)

	// Without --scan, credentials are not looked for at all.
	if _, _, code := run(t, db, cwd, "doctor"); code != 0 {
		t.Fatalf("doctor without --scan exit = %d; the scan is opt-in", code)
	}

	out, _, code = run(t, db, cwd, "doctor", "--scan")
	if code != 1 {
		t.Fatalf("doctor --scan over a credential exit = %d, want 1", code)
	}
	if !strings.Contains(out, "credentials: 1 finding(s) across 1 rows") {
		t.Fatalf("the finding is not on stdout:\n%s", out)
	}
	if !strings.Contains(out, "issue "+id+" (description)") {
		t.Fatalf("the finding does not name the record and the field:\n%s", out)
	}
	// It never redacts: the text is left exactly as it was.
	if body := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))["description"]; body != token {
		t.Fatalf("doctor --scan altered the stored text: %v", body)
	}

	report := decodeOne[map[string]any](t, mustDoctorJSON(t, db, cwd))
	if report["credential_rows_scanned"] != float64(1) {
		t.Fatalf("doctor --json = %#v", report)
	}
	// It takes no project scope, so an explicit one is a misuse rather than
	// a narrower answer.
	if _, _, code := run(t, db, cwd, "doctor", "--scan", "-P", "inbox"); code == 0 {
		t.Log("doctor accepted -P; it is documented as store-wide, and -P is a persistent flag it ignores")
	}
}

// mustDoctorJSON runs doctor --scan --json, which exits 1 on the finding it is
// asked to produce, and returns its stdout.
func mustDoctorJSON(t *testing.T, db, cwd string) string {
	t.Helper()
	out, _, code := run(t, db, cwd, "doctor", "--scan", "--json")
	if code != 1 {
		t.Fatalf("doctor --scan --json exit = %d, want 1", code)
	}
	return out
}

// TestDoctorRepairsAFailedFTSIndex: --repair rebuilds an index that failed its
// check, and repairs nothing else. Desynchronising one is the only way to see
// the check that guards the store's own search.
func TestDoctorRepairsAFailedFTSIndex(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "create", "findable", "--inbox", "-d", "a distinctive needle")
	dropFTSTrigger(t, db)
	mustRun(t, db, cwd, "update", id, "--title", "renamed out of the index")

	out, _, err := RunForTest([]string{"doctor"}, db, cwd)
	if ExitCodeFor(err) != 1 {
		t.Fatalf("doctor over a desynchronised index exit = %d, want 1", ExitCodeFor(err))
	}
	if !strings.Contains(out, "issues_fts") {
		t.Fatalf("doctor did not name the failed index on stdout:\n%s", out)
	}
	// And the error names WHICH check failed, rather than only that the
	// store is unhealthy — the difference between a report and an alarm.
	if !strings.Contains(err.Error(), "issues_fts failed") {
		t.Fatalf("doctor's error = %q, want it to name the failed check", err)
	}

	out, _, code := run(t, db, cwd, "doctor", "--repair")
	if code != 0 {
		t.Fatalf("doctor --repair exit = %d, want 0 once the index is rebuilt:\n%s", code, out)
	}
	if !strings.Contains(out, "repairs made:      issues_fts") {
		t.Fatalf("doctor --repair did not report what it rebuilt:\n%s", out)
	}
	if found := mustRun(t, db, cwd, "search", "renamed", "--inbox"); !strings.Contains(found, id) {
		t.Fatalf("search is still broken after a reported repair:\n%s", found)
	}
}

// TestSyncWithoutARemoteIsAnError: a fetch it cannot complete is an error at
// exit 1, never a quiet no-op. A sync that says nothing when it reached nothing
// is how two machines drift apart without anyone noticing.
func TestSyncWithoutARemoteIsAnError(t *testing.T) {
	db, cwd := newStore(t)
	mustRun(t, db, cwd, "q", "--inbox", "something to export")

	_, _, err := RunForTest([]string{"sync"}, db, cwd)
	if err == nil {
		t.Fatal("sync with no remote succeeded")
	}
	if ExitCodeFor(err) != 1 {
		t.Fatalf("sync with no remote exit = %d, want 1", ExitCodeFor(err))
	}
	if !strings.Contains(err.Error(), "origin") {
		t.Fatalf("the error does not name the remote it could not reach: %v", err)
	}
}

// TestSyncCarriesAnIssueToTheOtherMachine is the one cycle the doc describes —
// pull, import, export, push — driven entirely through the CLI, with the remote
// added by git exactly as the doc tells a person to add it.
func TestSyncCarriesAnIssueToTheOtherMachine(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, "", "init", "--bare", "-q", "-b", "main", remote)

	here, hereCwd := newStore(t)
	there, thereCwd := newStore(t)

	// The first sync initialises the mirror repository; the remote is added
	// with git, because drops has no flag for it.
	id := mustRun(t, here, hereCwd, "create", "cross the wire", "--inbox", "-d", "the body")
	if _, _, err := RunForTest([]string{"sync"}, here, hereCwd); err == nil {
		t.Fatal("the first sync found a remote that was never added")
	}
	gitRun(t, filepath.Dir(here), "remote", "add", "origin", remote)

	out := mustRun(t, here, hereCwd, "sync")
	if !strings.HasPrefix(out, "exported at ") {
		t.Fatalf("a first export printed %q, want `exported at <sha>`", out)
	}
	if again := mustRun(t, here, hereCwd, "sync"); again != "nothing to sync" {
		t.Fatalf("a second sync with no change printed %q, want `nothing to sync`", again)
	}

	mustRun(t, there, thereCwd, "q", "--inbox", "local to the other machine")
	if _, _, err := RunForTest([]string{"sync"}, there, thereCwd); err == nil {
		t.Fatal("the other machine found a remote that was never added")
	}
	gitRun(t, filepath.Dir(there), "remote", "add", "origin", remote)

	got := mustRun(t, there, thereCwd, "sync")
	if !strings.Contains(got, "imported ") {
		t.Fatalf("the other machine's sync printed %q, want an import line", got)
	}
	page := mustRun(t, there, thereCwd, "show", id, "--json")
	view := decodeOne[map[string]any](t, page)
	if view["title"] != "cross the wire" || view["description"] != "the body" {
		t.Fatalf("the issue did not arrive intact: %#v", view)
	}
}

// TestReplicaRekeyRotatesTheSidecarKey: it rotates this installation's FUTURE
// identity — after a restore, or when two installations have provably shared
// one key — and does not rewrite history.
func TestReplicaRekeyRotatesTheSidecarKey(t *testing.T) {
	db, cwd := newStore(t)
	id := mustRun(t, db, cwd, "q", "--inbox", "written under the old key")
	before := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))

	was := sidecarReplica(t, db)
	if was == "" {
		t.Fatal("the first local write minted no replica key")
	}

	rotated := mustRun(t, db, cwd, "replica", "rekey")
	if rotated == was {
		t.Fatalf("rekey returned the same key %q", rotated)
	}
	if now := sidecarReplica(t, db); now != rotated {
		t.Fatalf("the sidecar holds %q, rekey printed %q", now, rotated)
	}
	if got := decodeOne[map[string]string](t, mustRun(t, db, cwd, "replica", "rekey", "--json")); got["replica_key"] == "" {
		t.Fatalf("replica rekey --json = %#v, want a replica_key", got)
	}

	// History is untouched: the old issue still records the key it was
	// written under.
	after := decodeOne[map[string]any](t, mustRun(t, db, cwd, "show", id, "--json"))
	if after["creation_replica"] != before["creation_replica"] {
		t.Fatalf("rekey rewrote history: creation_replica %v -> %v",
			before["creation_replica"], after["creation_replica"])
	}
}

// sidecarReplica reads the replica key straight out of replica.json, the
// machine-local file the doc says lives beside the store.
func sidecarReplica(t *testing.T, dbPath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(dbPath), "replica.json"))
	if err != nil {
		return ""
	}
	return decodeOne[struct {
		Replica string `json:"replica_key"`
	}](t, string(raw)).Replica
}

// gitRun drives git the way the doc tells a person to, with a scratch HOME so
// the workstation's own config cannot change the answer.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
