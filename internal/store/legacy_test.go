package store_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// The v6 fixture is built from the schema measured off the real legacy store
// (testdata/legacy_v6.sql, a verbatim .schema dump), so the conversion is
// exercised against the shape it will actually meet rather than a paraphrase.

//nolint:gosec // fixture SQL, not user input
const legacyRows = `
INSERT INTO projects (id, slug, repo_path, remote_url, archived_at, created_at) VALUES
  (1, 'drops',   '/Volumes/ted/src/drops',  'git@github.com:tedkulp/drops.git', NULL, '2026-01-01T00:00:00Z'),
  (2, 'beacon',  '/Volumes/ted/src/beacon', NULL, NULL,                    '2026-01-02T00:00:00Z'),
  (3, 'retired', NULL,                      NULL, '2026-05-01T00:00:00Z',  '2026-01-03T00:00:00Z');

INSERT INTO issues (id, project_id, title, description, issue_type, status, priority,
                    assignee, close_reason, deferred_until, created_at, updated_at,
                    closed_at, started_at, metadata) VALUES
  ('dw32p',    1, 'the map',           'body',  'epic',     'open',        1, NULL,  NULL,       NULL, '2026-02-01T00:00:00Z', '2026-02-10T00:00:00Z', NULL, NULL, NULL),
  ('dw32p.1',  1, 'agreeing child',    '',      'task',     'closed',      2, 'ted', 'answered', NULL, '2026-02-02T00:00:00Z', '2026-02-11T00:00:00Z', '2026-02-11T00:00:00Z', NULL, NULL),
  ('dw32p.2',  1, 'dotted-only child', '',      'task',     'in_progress', 2, NULL,  NULL,       NULL, '2026-02-03T00:00:00Z', '2026-02-12T00:00:00Z', NULL, '2026-02-12T00:00:00Z', NULL),
  ('dw32p.3',  2, 'wrong-Project child', '',    'task',     'deleted',     2, NULL,  NULL,       NULL, '2026-02-04T00:00:00Z', '2026-02-13T00:00:00Z', NULL, NULL, NULL),
  ('br-6vf',   1, 'explicit-only child', '',    'bug',      'deleted',     3, NULL,  NULL,       NULL, '2026-02-05T00:00:00Z', '2026-02-14T00:00:00Z', '2026-02-14T00:00:00Z', NULL, NULL),
  ('k3f9x',    2, 'an unrelated Issue', 'text', 'research', 'open',        0, NULL,  NULL,       '2026-12-01T00:00:00Z', '2026-02-06T00:00:00Z', '2026-02-15T00:00:00Z', NULL, NULL, NULL);

INSERT INTO dependencies (from_id, to_id, dep_type, created_at) VALUES
  ('dw32p.1', 'dw32p', 'parent-child',    '2026-02-02T00:00:01Z'),
  ('br-6vf',  'dw32p', 'parent-child',    '2026-02-05T00:00:01Z'),
  ('k3f9x',   'dw32p', 'blocks',          '2026-02-06T00:00:01Z'),
  ('k3f9x',   'dw32p.1', 'related',       '2026-02-06T00:00:02Z'),
  ('dw32p.2', 'k3f9x', 'discovered-from', '2026-02-06T00:00:03Z');

INSERT INTO labels (issue_id, label) VALUES
  ('dw32p', 'wayfinder:map'),
  ('dw32p.1', 'wayfinder:task');

INSERT INTO comments (id, issue_id, author, body, created_at) VALUES
  ('dw32p:0011223344ff', 'dw32p', 'ted', 'a note on the map', '2026-02-07T00:00:00Z'),
  ('dw32p:aabbccddeeff', 'dw32p', 'migration', 'a second note',   '2026-02-08T00:00:00Z');

INSERT INTO memories (id, project_id, kind, title, body, ambient, salience, source_project,
                      source, created_at, updated_at, last_recalled_at, recall_count,
                      deleted_at, superseded_by) VALUES
  ('br-et8',  1,    'lesson',    'the current lesson', 'current body', 0, 3, 'drops', 'a skill', '2026-03-01T00:00:00Z', '2026-03-02T00:00:00Z', NULL, 0, NULL, NULL),
  ('br-eyp',  1,    'lesson',    'the retired lesson', 'old body',     0, 3, 'drops', 'a skill', '2026-03-03T00:00:00Z', '2026-03-04T00:00:00Z', NULL, 0, NULL, 'br-et8'),
  ('br-9nf',  NULL, 'gotcha',    'an unscoped lesson', 'global body',  0, 5, 'drops', '',        '2026-03-05T00:00:00Z', '2026-03-06T00:00:00Z', NULL, 0, NULL, NULL),
  ('br-dnb',  2,    'reference', 'an obsolete note',   'stale body',   0, 1, 'beacon', '',       '2026-03-07T00:00:00Z', '2026-03-08T00:00:00Z', NULL, 0, NULL, NULL);

INSERT INTO sync_state (id, dirty_at, last_export_at, last_commit_at, last_commit_sha,
                        last_dolt_warn_at, last_export_ms, last_sync_unlocked)
  VALUES (1, '2026-03-09T00:00:00Z', '2026-03-08T00:00:00Z', '2026-03-08T00:00:00Z', 'deadbeef', NULL, 45, 0);
`

// seedLegacy builds a v6 store at a temporary path and returns it.
func seedLegacy(t *testing.T, extra ...string) string {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join("testdata", "legacy_v6.sql"))
	if err != nil {
		t.Fatalf("read legacy schema: %v", err)
	}
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("create legacy store: %v", err)
	}
	defer raw.Close()
	for _, script := range append([]string{string(schema), legacyRows}, extra...) {
		if _, err := raw.Exec(script); err != nil {
			t.Fatalf("seed legacy store: %v", err)
		}
	}
	return path
}

func TestRewriteConvertsALegacyStoreInPlace(t *testing.T) {
	path := seedLegacy(t)
	replica := newReplicaKey(t)

	report, err := store.Rewrite(t.Context(), path, store.LegacyPlan{
		Replica:  replica,
		Locators: map[string]string{"drops": "github.com/tedkulp/drops"},
	})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	// Three legacy Projects plus the two reserved ones the legacy store lacked.
	want := store.LegacyCounts{
		Projects: 5, Issues: 6, IssueParents: 3, OmittedParents: 1,
		Dependencies: 3, Labels: 2, Comments: 2, Memories: 4,
	}
	if report.LegacyCounts != want {
		t.Errorf("conversion counts = %+v, want %+v", report.LegacyCounts, want)
	}

	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open the converted store: %v", err)
	}
	defer opened.Close()

	if version := scalar[int](t, inspect(t, path), "PRAGMA user_version"); version != 7 {
		t.Errorf("converted user_version = %d, want 7", version)
	}

	// Reserved Projects take their fixed keys wherever the conversion runs.
	global, err := opened.ProjectBySlug(t.Context(), model.GlobalProjectSlug)
	if err != nil {
		t.Fatalf("read the global Project: %v", err)
	}
	if global.Key != model.GlobalProjectKey {
		t.Errorf("global Project key = %s, want the reserved %s", global.Key, model.GlobalProjectKey)
	}

	// The archived Project keeps its archival, and repo_path is not carried.
	retired, err := opened.ProjectBySlug(t.Context(), "retired")
	if err != nil {
		t.Fatalf("read the archived Project: %v", err)
	}
	if retired.ArchivedAt == nil || *retired.ArchivedAt != "2026-05-01T00:00:00Z" {
		t.Errorf("archived_at = %v, want it preserved", retired.ArchivedAt)
	}

	// remote_url becomes a locator only where the plan normalized one.
	drops, err := opened.ProjectBySlug(t.Context(), "drops")
	if err != nil {
		t.Fatalf("read the drops Project: %v", err)
	}
	locators, err := opened.ProjectLocators(t.Context(), drops.Key)
	if err != nil {
		t.Fatalf("read locators: %v", err)
	}
	if len(locators) != 1 || locators[0].Locator != "github.com/tedkulp/drops" {
		t.Errorf("locators = %+v, want the one the plan named", locators)
	}
}

// TestRewriteCarriesIssueFieldsAndConvertsRemovedStatuses pins the two v6
// lifecycle values that leave the enum: "deleted" becomes a tombstone over the
// lifecycle the Issue actually reached, and nothing else changes.
func TestRewriteCarriesIssueFieldsAndConvertsRemovedStatuses(t *testing.T) {
	path := seedLegacy(t)
	replica := newReplicaKey(t)
	if _, err := store.Rewrite(t.Context(), path, store.LegacyPlan{Replica: replica}); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open the converted store: %v", err)
	}
	defer opened.Close()

	cases := []struct {
		id        model.ID
		status    model.Status
		tombstone model.Tombstone
	}{
		{"dw32p", model.StatusOpen, model.Live},
		{"dw32p.1", model.StatusClosed, model.Live},
		{"dw32p.2", model.StatusInProgress, model.Live},
		// Removed with no closed_at: the lifecycle it actually reached was open.
		{"dw32p.3", model.StatusOpen, model.Tombstoned},
		// Removed carrying a closed_at: it had reached closed.
		{"br-6vf", model.StatusClosed, model.Tombstoned},
	}
	for _, testCase := range cases {
		issue, err := opened.Issue(t.Context(), testCase.id)
		if err != nil {
			t.Fatalf("read Issue %s: %v", testCase.id, err)
		}
		if issue.Status != testCase.status || issue.Tombstone != testCase.tombstone {
			t.Errorf("Issue %s: status %s tombstone %v, want %s / %v",
				testCase.id, issue.Status, issue.Tombstone, testCase.status, testCase.tombstone)
		}
		if issue.CreationReplica != replica {
			t.Errorf("Issue %s creation replica = %s, want the cutover %s",
				testCase.id, issue.CreationReplica, replica)
		}
		if issue.Revision.Generation != 1 || issue.Revision.Replica != replica {
			t.Errorf("Issue %s revision = %+v, want generation 1 under the cutover Replica",
				testCase.id, issue.Revision)
		}
	}

	carried, err := opened.Issue(t.Context(), "k3f9x")
	if err != nil {
		t.Fatalf("read Issue: %v", err)
	}
	if carried.DeferredUntil == nil || *carried.DeferredUntil != "2026-12-01T00:00:00Z" {
		t.Errorf("deferred_until = %v, want it preserved despite having no writer", carried.DeferredUntil)
	}
	started, err := opened.Issue(t.Context(), "dw32p.2")
	if err != nil {
		t.Fatalf("read Issue: %v", err)
	}
	if started.StartedAt == nil || *started.StartedAt != "2026-02-12T00:00:00Z" {
		t.Errorf("started_at = %v, want it preserved byte for byte", started.StartedAt)
	}
}

// TestRewriteReconcilesParentage exercises all four cases the reconciliation
// decision named: an explicit edge that agrees with the ID spelling, an explicit
// edge with no spelling, a same-Project spelling with no edge, and a
// cross-Project spelling on a removed Issue, which is dropped.
func TestRewriteReconcilesParentage(t *testing.T) {
	path := seedLegacy(t)
	report, err := store.Rewrite(t.Context(), path, store.LegacyPlan{Replica: newReplicaKey(t)})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open the converted store: %v", err)
	}
	defer opened.Close()

	cases := map[model.ID]struct {
		parent    model.ID
		createdAt model.Timestamp
	}{
		// Explicit edge agreeing with the spelling: the edge's own timestamp.
		"dw32p.1": {"dw32p", "2026-02-02T00:00:01Z"},
		// Explicit edge with no spelling to agree with.
		"br-6vf": {"dw32p", "2026-02-05T00:00:01Z"},
		// Spelling only, same Project: minting the child ID is the relation.
		"dw32p.2": {"dw32p", "2026-02-03T00:00:00Z"},
	}
	for child, want := range cases {
		record, err := opened.IssueParent(t.Context(), child)
		if err != nil {
			t.Fatalf("read parent of %s: %v", child, err)
		}
		if record.ParentID != want.parent || record.CreatedAt != want.createdAt {
			t.Errorf("parent of %s = %s at %s, want %s at %s",
				child, record.ParentID, record.CreatedAt, want.parent, want.createdAt)
		}
		if record.Tombstone != model.Live {
			t.Errorf("parent of %s is tombstoned; relation and Issue tombstones are independent", child)
		}
	}

	// The cross-Project spelling on a removed Issue is an artifact of the legacy
	// create --parent bug, not a relationship.
	if _, err := opened.IssueParent(t.Context(), "dw32p.3"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("parent of the wrong-Project child: err = %v, want ErrNotFound", err)
	}
	if !slices.Equal(report.OmittedParents, []model.ID{"dw32p.3"}) {
		t.Errorf("omitted parents = %v, want [dw32p.3]", report.OmittedParents)
	}

	// A retained relation stays live even though its child Issue is removed.
	removed, err := opened.IssueParent(t.Context(), "br-6vf")
	if err != nil {
		t.Fatalf("read parent of a removed Issue: %v", err)
	}
	if removed.Tombstone != model.Live {
		t.Error("removing an Issue erased its place in the historical graph")
	}
}

// TestRewriteRefusesAmbiguousParentage pins the two graphs the decision says
// must roll the conversion back rather than be resolved on a guess.
func TestRewriteRefusesAmbiguousParentage(t *testing.T) {
	cases := map[string]string{
		"a live cross-Project spelling": `
			INSERT INTO issues (id, project_id, title, description, issue_type, status,
			                    priority, created_at, updated_at)
			VALUES ('dw32p.9', 2, 'live in the wrong Project', '', 'task', 'open', 2,
			        '2026-02-20T00:00:00Z', '2026-02-20T00:00:00Z');`,
		"an edge that contradicts the spelling": `
			INSERT INTO dependencies (from_id, to_id, dep_type, created_at)
			VALUES ('dw32p.2', 'k3f9x', 'parent-child', '2026-02-21T00:00:00Z');`,
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			path := seedLegacy(t, extra)
			_, err := store.Rewrite(t.Context(), path, store.LegacyPlan{Replica: newReplicaKey(t)})
			if !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("rewrite: err = %v, want ErrInvalid", err)
			}
			if version := scalar[int](t, inspect(t, path), "PRAGMA user_version"); version != 6 {
				t.Errorf("refused conversion left the store at version %d, want 6 untouched", version)
			}
		})
	}
}

func TestRewriteCuratesMemoriesThroughThePlan(t *testing.T) {
	path := seedLegacy(t)
	report, err := store.Rewrite(t.Context(), path, store.LegacyPlan{
		Replica: newReplicaKey(t),
		Memories: func(legacy store.LegacyMemory) (store.MemoryPlan, bool) {
			if legacy.ID == "br-dnb" {
				return store.MemoryPlan{}, false
			}
			slug := legacy.ProjectSlug
			if slug == "" {
				// A subject-specific unscoped memory moves to the Project it
				// describes, rather than staying global by default.
				slug = "ansible"
			}
			return store.MemoryPlan{
				ProjectSlug:  slug,
				Title:        legacy.Title,
				Body:         legacy.Kind + ": " + legacy.Body,
				SupersededBy: legacy.SupersededBy,
			}, true
		},
	})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if report.Memories != 3 || report.LegacyCounts.OmittedMemories != 1 {
		t.Errorf("memories kept %d dropped %d, want 3 and 1",
			report.Memories, report.LegacyCounts.OmittedMemories)
	}
	if !slices.Equal(report.OmittedMemories, []model.ID{"br-dnb"}) {
		t.Errorf("omitted memories = %v, want [br-dnb]", report.OmittedMemories)
	}

	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open the converted store: %v", err)
	}
	defer opened.Close()

	// An omitted memory is absent, not tombstoned.
	if _, err := opened.Memory(t.Context(), "br-dnb"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("dropped memory: err = %v, want ErrNotFound", err)
	}

	// The Project the curation asked for was created for it.
	moved, err := opened.Memory(t.Context(), "br-9nf")
	if err != nil {
		t.Fatalf("read the moved memory: %v", err)
	}
	ansible, err := opened.ProjectBySlug(t.Context(), "ansible")
	if err != nil {
		t.Fatalf("read the created Project: %v", err)
	}
	if moved.ProjectKey != ansible.Key {
		t.Errorf("moved memory belongs to %s, want the created ansible Project", moved.ProjectKey)
	}
	if moved.Body != "gotcha: global body" {
		t.Errorf("body = %q, want the legacy kind folded into it", moved.Body)
	}

	// The supersession edge survives, and both ends are retained.
	retired, err := opened.Memory(t.Context(), "br-eyp")
	if err != nil {
		t.Fatalf("read the superseded memory: %v", err)
	}
	if retired.SupersededBy == nil || *retired.SupersededBy != "br-et8" {
		t.Errorf("superseded_by = %v, want br-et8", retired.SupersededBy)
	}
}

// TestRewriteRefusesADanglingSupersession pins that curation cannot leave a
// retained memory pointing at one it dropped.
func TestRewriteRefusesADanglingSupersession(t *testing.T) {
	path := seedLegacy(t)
	_, err := store.Rewrite(t.Context(), path, store.LegacyPlan{
		Replica: newReplicaKey(t),
		Memories: func(legacy store.LegacyMemory) (store.MemoryPlan, bool) {
			if legacy.ID == "br-et8" {
				return store.MemoryPlan{}, false
			}
			slug := legacy.ProjectSlug
			if slug == "" {
				slug = model.GlobalProjectSlug
			}
			return store.MemoryPlan{
				ProjectSlug: slug, Title: legacy.Title, Body: legacy.Body,
				SupersededBy: legacy.SupersededBy,
			}, true
		},
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("rewrite: err = %v, want ErrInvalid", err)
	}
}

// TestRewriteRollsBackOnAnUnmetExpectation pins the assertion the authoritative
// cutover leans on: measured counts that do not match abandon the whole thing.
func TestRewriteRollsBackOnAnUnmetExpectation(t *testing.T) {
	path := seedLegacy(t)
	_, err := store.Rewrite(t.Context(), path, store.LegacyPlan{
		Replica: newReplicaKey(t),
		Expect:  &store.LegacyCounts{Projects: 5, Issues: 999},
	})
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("rewrite: err = %v, want ErrConflict", err)
	}
	if version := scalar[int](t, inspect(t, path), "PRAGMA user_version"); version != 6 {
		t.Errorf("rolled-back conversion left the store at version %d, want 6", version)
	}
	raw := inspect(t, path)
	if rows := scalar[int](t, raw, "SELECT count(*) FROM issues"); rows != 6 {
		t.Errorf("legacy issues after rollback = %d, want the original 6", rows)
	}
}

func TestRewriteRefusesAnythingButTheMeasuredV6Shape(t *testing.T) {
	cases := map[string]string{
		"a column the conversion has not seen": `ALTER TABLE issues ADD COLUMN sprint TEXT;`,
		"a table the conversion has not seen":  `CREATE TABLE sprints (id TEXT PRIMARY KEY);`,
		"a missing table":                      `DROP TABLE labels;`,
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			path := seedLegacy(t, extra)
			_, err := store.Rewrite(t.Context(), path, store.LegacyPlan{Replica: newReplicaKey(t)})
			if !errors.Is(err, store.ErrUnsupportedSchema) {
				t.Errorf("rewrite: err = %v, want ErrUnsupportedSchema", err)
			}
		})
	}

	fresh := filepath.Join(t.TempDir(), "fresh.db")
	opened, err := store.Open(t.Context(), fresh)
	if err != nil {
		t.Fatalf("open a fresh store: %v", err)
	}
	opened.Close()
	if _, err := store.Rewrite(t.Context(), fresh, store.LegacyPlan{Replica: newReplicaKey(t)}); !errors.Is(err, store.ErrUnsupportedSchema) {
		t.Errorf("rewriting an already-converted store: err = %v, want ErrUnsupportedSchema", err)
	}
}

// TestConvertedStoreIsSearchableAndDirty pins the two things the conversion owes
// the rest of the build: the FTS indexes are populated by the writes rather than
// carried over, and the store knows it has never been exported.
func TestConvertedStoreIsSearchableAndDirty(t *testing.T) {
	path := seedLegacy(t)
	if _, err := store.Rewrite(t.Context(), path, store.LegacyPlan{Replica: newReplicaKey(t)}); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open the converted store: %v", err)
	}
	defer opened.Close()

	found, err := opened.SearchIssues(t.Context(), "unrelated", store.IssueFilter{})
	if err != nil {
		t.Fatalf("search the converted store: %v", err)
	}
	if got := issueIDs(found); !slices.Equal(got, []model.ID{"k3f9x"}) {
		t.Errorf("search = %v, want [k3f9x]", got)
	}
	inThread, err := opened.SearchIssues(t.Context(), "second note", store.IssueFilter{})
	if err != nil {
		t.Fatalf("search comments: %v", err)
	}
	if got := issueIDs(inThread); !slices.Equal(got, []model.ID{"dw32p"}) {
		t.Errorf("comment search = %v, want [dw32p]", got)
	}

	for _, index := range store.FTSIndexes() {
		if err := opened.CheckFTS(t.Context(), index); err != nil {
			t.Errorf("%s is desynced after the conversion: %v", index, err)
		}
	}

	state := stateOf(t, opened)
	if state.Exported() || state.ExportedWriteSeq != 0 || state.LastExportHead != "" {
		t.Errorf("converted store state = %+v, want unexported so the first sync exports everything", state)
	}
	if state.WriteSeq == 0 {
		t.Error("converted store counted no writes")
	}
}

// TestRewriteAgainstTheRealLegacyStore converts a copy of the authoritative v6
// store when one is pointed at, and asserts the counts the reconciliation and
// curation decisions measured off it.
//
// It SKIPS without DROPS_LEGACY_V6, and a skipped package still prints "ok", so
// this is not coverage the gate provides. It is the rehearsal the cutover runs
// before it touches the real thing.
func TestRewriteAgainstTheRealLegacyStore(t *testing.T) {
	source := os.Getenv("DROPS_LEGACY_V6")
	if source == "" {
		t.Skip("set DROPS_LEGACY_V6 to a copy of the authoritative v6 store to run this")
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	path := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("copy the legacy store: %v", err)
	}

	report, err := store.Rewrite(t.Context(), path, store.LegacyPlan{Replica: newReplicaKey(t)})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	// Measured off the authoritative store on 2026-09-02, and asserted by the
	// parentage reconciliation decision: 549 retained relations, nine omitted
	// artifacts of the legacy create --parent bug.
	if report.IssueParents != 549 || report.LegacyCounts.OmittedParents != 9 {
		t.Errorf("parentage: %d retained, %d omitted; want 549 and 9",
			report.IssueParents, report.LegacyCounts.OmittedParents)
	}
	if report.Issues != 1025 {
		t.Errorf("issues = %d, want 1025", report.Issues)
	}
	if report.Dependencies != 462 {
		t.Errorf("dependencies = %d, want 462 (946 rows less 484 parent-child edges)", report.Dependencies)
	}
	if report.Comments != 181 || report.Labels != 146 || report.Memories != 149 {
		t.Errorf("comments %d labels %d memories %d; want 181, 146, 149",
			report.Comments, report.Labels, report.Memories)
	}

	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open the converted store: %v", err)
	}
	defer opened.Close()
	for _, index := range store.FTSIndexes() {
		if err := opened.CheckFTS(t.Context(), index); err != nil {
			t.Errorf("%s is desynced after converting the real store: %v", index, err)
		}
	}
	if violations, err := opened.ForeignKeyCheck(t.Context()); err != nil || len(violations) != 0 {
		t.Errorf("foreign key check after conversion: %v, %v", violations, err)
	}
}
