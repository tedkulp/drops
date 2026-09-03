package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
	dropsync "github.com/tedkulp/drops/internal/sync"

	_ "modernc.org/sqlite"
)

// legacySchema is the same verbatim v6 dump internal/store converts against.
// Sharing the file rather than a second copy means a schema this tool has
// drifted from cannot look correct here.
const legacySchema = "../../internal/store/testdata/legacy_v6.sql"

// seedV6 builds a v6 store carrying one row of every parentage shape the rule
// distinguishes, plus the memories the curation has an opinion about.
func seedV6(t *testing.T, extra ...string) string {
	t.Helper()
	schema, err := os.ReadFile(legacySchema)
	if err != nil {
		t.Fatalf("read the v6 schema: %v", err)
	}
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create the v6 store: %v", err)
	}
	defer db.Close()
	for _, script := range append([]string{string(schema), v6Rows}, extra...) {
		if _, err := db.Exec(script); err != nil {
			t.Fatalf("seed the v6 store: %v", err)
		}
	}
	return path
}

// The rows exercise, in order: an explicit parent-child edge; a dotted-only
// child in its parent's Project; a dotted-only child in another Project whose
// own Issue is tombstoned; a dotted spelling whose parent does not exist; an
// ordinary blocks dependency; and three memories the curation retains, drops
// and has never seen.
const v6Rows = `
INSERT INTO projects (id, slug, repo_path, remote_url, created_at) VALUES
  (1, 'drops',  '/home/ted/src/drops',  'git@github.com:tedkulp/drops.git', '2026-01-01T00:00:00Z'),
  (2, 'beacon', '/home/ted/src/beacon', '',                                 '2026-01-02T00:00:00Z');

INSERT INTO issues (id, project_id, title, description, issue_type, status, priority, created_at, updated_at) VALUES
  ('root',     1, 'root',            '', 'epic', 'open',    1, '2026-02-01T00:00:00Z', '2026-02-01T00:00:00Z'),
  ('root.1',   1, 'explicit child',  '', 'task', 'open',    2, '2026-02-02T00:00:00Z', '2026-02-02T00:00:00Z'),
  ('root.2',   1, 'dotted child',    '', 'task', 'closed',  2, '2026-02-03T00:00:00Z', '2026-02-03T00:00:00Z'),
  ('root.3',   2, 'wrong project',   '', 'task', 'deleted', 2, '2026-02-04T00:00:00Z', '2026-02-04T00:00:00Z'),
  ('orphan.9', 1, 'no such parent',  '', 'task', 'open',    2, '2026-02-05T00:00:00Z', '2026-02-05T00:00:00Z'),
  ('solo',     2, 'unrelated',       '', 'task', 'open',    2, '2026-02-06T00:00:00Z', '2026-02-06T00:00:00Z');

INSERT INTO dependencies (from_id, to_id, dep_type, created_at) VALUES
  ('root.1', 'root', 'parent-child', '2026-02-02T00:00:00Z'),
  ('solo',   'root', 'blocks',       '2026-02-06T00:00:00Z');

INSERT INTO labels (issue_id, label) VALUES ('root', 'wayfinder:map');

INSERT INTO comments (id, issue_id, author, body, created_at) VALUES
  ('c1', 'root', 'ted', 'a comment', '2026-02-07T00:00:00Z');

INSERT INTO memories (id, project_id, kind, title, body, ambient, salience, source_project,
                      source, created_at, updated_at, last_recalled_at, recall_count,
                      deleted_at, superseded_by) VALUES
  ('br-et8',  1, 'lesson', 'kept',    'a retained lesson.', 0, 3, 'drops', 'a skill',
   '2026-03-01T00:00:00Z', '2026-03-01T00:00:00Z', NULL, 0, NULL, NULL),
  ('br-dnb',  2, 'reference', 'gone',  'an obsolete note.', 0, 1, 'beacon', '',
   '2026-03-02T00:00:00Z', '2026-03-02T00:00:00Z', NULL, 0, NULL, NULL),
  ('br-ksvc', NULL, 'lesson', 'moved', 'an ansible lesson.', 0, 3, '', '',
   '2026-03-03T00:00:00Z', '2026-03-03T00:00:00Z', NULL, 0, NULL, NULL),
  ('later-1', 2, 'gotcha', 'unseen',   'written after the measurement.', 0, 3, '', '',
   '2026-03-04T00:00:00Z', '2026-03-04T00:00:00Z', NULL, 0, NULL, NULL);
`

func TestCensusCountsWhatTheConversionShouldWrite(t *testing.T) {
	census, err := TakeCensus(t.Context(), seedV6(t))
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}

	// drops and beacon, the two reserved slugs the v6 store lacks, and ansible,
	// which only a relocated memory needs.
	want := store.LegacyCounts{
		Projects: 5, Issues: 6, IssueParents: 2, OmittedParents: 1,
		Dependencies: 1, Labels: 1, Comments: 1, Memories: 3, OmittedMemories: 1,
	}
	if census.Counts != want {
		t.Errorf("census = %+v, want %+v", census.Counts, want)
	}
	if len(census.AmbiguousParents) != 0 {
		t.Errorf("census found ambiguous parents %v in a store that has none", census.AmbiguousParents)
	}
	if census.Remotes["drops"] != "git@github.com:tedkulp/drops.git" {
		t.Errorf("remotes = %v, want the raw drops remote", census.Remotes)
	}
}

// An explicit edge is authoritative, so its child must not also be inferred
// from its spelling and counted twice.
func TestCensusCountsAnExplicitlyParentedChildOnce(t *testing.T) {
	census, err := TakeCensus(t.Context(), seedV6(t))
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if census.Counts.IssueParents != 2 {
		t.Errorf("IssueParents = %d, want 2: root.1 by its edge and root.2 by its spelling",
			census.Counts.IssueParents)
	}
}

// A dotted spelling naming no existing Issue is not a relation at all.
func TestCensusIgnoresADottedIDWithNoParent(t *testing.T) {
	census, err := TakeCensus(t.Context(), seedV6(t, `
		INSERT INTO issues (id, project_id, title, description, issue_type, status, priority, created_at, updated_at)
		  VALUES ('missing.4', 1, 'no parent', '', 'task', 'open', 2, '2026-02-08T00:00:00Z', '2026-02-08T00:00:00Z');`))
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if census.Counts.IssueParents != 2 || census.Counts.OmittedParents != 1 {
		t.Errorf("parentage = %d retained, %d omitted; an orphan dotted ID changed the count",
			census.Counts.IssueParents, census.Counts.OmittedParents)
	}
	if len(census.AmbiguousParents) != 0 {
		t.Errorf("an orphan dotted ID was read as ambiguous parentage: %v", census.AmbiguousParents)
	}
}

// dw32p.12 omits a cross-Project dotted-only pair only when its child is
// tombstoned. A live one is ambiguous and refuses migration.
func TestCensusRefusesALiveCrossProjectDottedChild(t *testing.T) {
	census, err := TakeCensus(t.Context(), seedV6(t, `
		INSERT INTO issues (id, project_id, title, description, issue_type, status, priority, created_at, updated_at)
		  VALUES ('root.4', 2, 'live and elsewhere', '', 'task', 'open', 2, '2026-02-09T00:00:00Z', '2026-02-09T00:00:00Z');`))
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if len(census.AmbiguousParents) != 1 || census.AmbiguousParents[0] != model.ID("root.4") {
		t.Fatalf("ambiguous parents = %v, want [root.4]", census.AmbiguousParents)
	}
	if census.Counts.OmittedParents != 1 {
		t.Errorf("OmittedParents = %d; a live cross-Project child is ambiguous, not omitted",
			census.Counts.OmittedParents)
	}

	// And the run refuses rather than converting on a guess.
	err = run(t.Context(), options{store: seedV6(t, `
		INSERT INTO issues (id, project_id, title, description, issue_type, status, priority, created_at, updated_at)
		  VALUES ('root.4', 2, 'live and elsewhere', '', 'task', 'open', 2, '2026-02-09T00:00:00Z', '2026-02-09T00:00:00Z');`),
		dryRun: true, out: os.Stdout})
	// store.Rewrite refuses this graph too, mid-transaction and one Issue at a
	// time. Asserting only that root.4 is named cannot tell this guard from
	// that one, and the difference is what the guard is for: it refuses before
	// the backup is taken and names every ambiguous child at once.
	if err == nil || !strings.Contains(err.Error(), "are not tombstoned") {
		t.Errorf("run() = %v, want the pre-flight refusal, not the conversion's", err)
	}
}

// Parent-child edges leave the dependency table in v7, so they must not still
// be counted as dependencies.
func TestCensusExcludesParentEdgesFromDependencies(t *testing.T) {
	census, err := TakeCensus(t.Context(), seedV6(t))
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if census.Counts.Dependencies != 1 {
		t.Errorf("Dependencies = %d, want 1: two rows less the parent-child edge",
			census.Counts.Dependencies)
	}
}

// The whole point of the census is to be handed to Rewrite as Expect, so the
// two independent readings have to agree on a real conversion.
func TestDryRunConvertsAndAgreesWithTheCensus(t *testing.T) {
	path := seedV6(t)
	census, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	plan, err := buildPlan(census)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	report, err := store.Rewrite(t.Context(), path, plan)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if report.LegacyCounts != census.Counts {
		t.Errorf("conversion wrote %+v, census expected %+v", report.LegacyCounts, census.Counts)
	}
	if len(report.SkippedRemotes) != 0 {
		t.Errorf("skipped remotes %v; the drops remote normalizes", report.SkippedRemotes)
	}
	if _, err := verify(t.Context(), path); err != nil {
		t.Errorf("verify: %v", err)
	}
}

// A conversion whose outcome disagrees with the census must write nothing.
func TestExpectRollsBackAConversionThatDisagrees(t *testing.T) {
	path := seedV6(t)
	census, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	plan, err := buildPlan(census)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	wrong := census.Counts
	wrong.Issues++
	plan.Expect = &wrong

	if _, err := store.Rewrite(t.Context(), path, plan); err == nil {
		t.Fatal("a conversion that disagreed with its plan committed")
	}
	// Still v6: the refusal wrote nothing.
	again, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("re-census after the refusal: %v", err)
	}
	if again.Counts != census.Counts {
		t.Errorf("the store changed under a refused conversion: %+v", again.Counts)
	}
}

func TestRunConvertsInPlaceAndWritesTheSidecar(t *testing.T) {
	path := seedV6(t)
	dir := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := run(t.Context(), options{store: path, out: os.Stdout}); err != nil {
		t.Fatalf("run: %v", err)
	}

	sidecar, err := dropsync.LoadSidecar(dir)
	if err != nil {
		t.Fatalf("load the sidecar: %v", err)
	}
	if sidecar == nil {
		t.Fatal("no replica sidecar beside the converted store")
	}
	if err := sidecar.Replica.Validate(); err != nil {
		t.Errorf("sidecar key %q: %v", sidecar.Replica, err)
	}

	// The conversion's creation origin and the machine's authoring identity are
	// the same key, or every converted record names an origin nothing claims.
	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open the converted store: %v", err)
	}
	defer opened.Close()
	owner, err := opened.IDOwner(t.Context(), model.ID("root"))
	if err != nil {
		t.Fatalf("read an ID owner: %v", err)
	}
	if owner.CreationReplica != sidecar.Replica {
		t.Errorf("root was created by %q but the sidecar authors as %q",
			owner.CreationReplica, sidecar.Replica)
	}

	if _, err := os.Stat(filepath.Join(dir, "issues.jsonl")); !os.IsNotExist(err) {
		t.Error("the v6 mirror is still beside the converted store")
	}
}

// A dry run is a rehearsal: it must leave the store it was pointed at exactly
// as it found it.
func TestDryRunTouchesNothing(t *testing.T) {
	path := seedV6(t)
	before, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if err := run(t.Context(), options{store: path, dryRun: true, out: os.Stdout}); err != nil {
		t.Fatalf("run: %v", err)
	}
	after, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("the dry run left the store unreadable as v6: %v", err)
	}
	if after.Counts != before.Counts {
		t.Errorf("the dry run changed the store: %+v became %+v", before.Counts, after.Counts)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "replica.json")); !os.IsNotExist(err) {
		t.Error("the dry run wrote a sidecar")
	}
}

func TestBackupIsProvedBeforeTheConversion(t *testing.T) {
	path := seedV6(t)
	census, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "backup")
	backup, err := Backup(t.Context(), path, dir, census)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	restored, err := TakeCensus(t.Context(), backup)
	if err != nil {
		t.Fatalf("census the backup: %v", err)
	}
	if restored.Counts != census.Counts {
		t.Errorf("backup holds %+v, store held %+v", restored.Counts, census.Counts)
	}
}

// The proof is the point: a backup that does not hold what the store holds is
// not a rollback, and finding that out later is finding it out too late.
func TestBackupRefusesWhenTheCopyDisagrees(t *testing.T) {
	path := seedV6(t)
	census, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	wrong := census
	wrong.Counts.Issues++
	if _, err := Backup(t.Context(), path, filepath.Join(t.TempDir(), "backup"), wrong); err == nil {
		t.Fatal("Backup accepted a copy that disagreed with the store")
	}
}

// Overwriting an existing backup would destroy the only other copy of the rows.
func TestBackupRefusesToOverwriteOne(t *testing.T) {
	path := seedV6(t)
	census, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "backup")
	if _, err := Backup(t.Context(), path, dir, census); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if _, err := Backup(t.Context(), path, dir, census); err == nil {
		t.Fatal("Backup overwrote an existing backup")
	}
}

// Expect is what makes the census more than a printout: without it a
// conversion that disagrees with the measurement still commits.
func TestBuildPlanAssertsTheCensus(t *testing.T) {
	census, err := TakeCensus(t.Context(), seedV6(t))
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	plan, err := buildPlan(census)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if plan.Expect == nil {
		t.Fatal("the plan asserts nothing; a conversion that disagrees would commit")
	}
	if *plan.Expect != census.Counts {
		t.Errorf("plan asserts %+v, census measured %+v", *plan.Expect, census.Counts)
	}
	if plan.Memories == nil {
		t.Error("the plan carries no curation, so every memory would migrate")
	}
	if err := plan.Replica.Validate(); err != nil {
		t.Errorf("plan replica %q: %v", plan.Replica, err)
	}
}

// dw32p.19 found the plain FTS check unable to fail in one direction, so all
// three indexes go through store.CheckFTS. A verify that quietly stopped
// checking them would still return cleanly.
func TestVerifyChecksEveryIndex(t *testing.T) {
	path := seedV6(t)
	census, err := TakeCensus(t.Context(), path)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	plan, err := buildPlan(census)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if _, err := store.Rewrite(t.Context(), path, plan); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	checked, err := verify(t.Context(), path)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	want := append([]string{"open", "foreign_keys"}, store.FTSIndexes()...)
	if len(checked) != len(want) {
		t.Fatalf("verify checked %v, want %v", checked, want)
	}
	for i, name := range want {
		if checked[i] != name {
			t.Errorf("verify checked %v, want %v", checked, want)
			break
		}
	}
}
