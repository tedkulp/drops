package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// TestRankOneFTSCheckCatchesBothDesyncDirections is the discriminating test the
// FTS contract needs. A healthy fixture proves nothing here: the check has to be
// run against an index that really has drifted from its content table, in both
// directions, and the plain form has to be shown missing it.
func TestRankOneFTSCheckCatchesBothDesyncDirections(t *testing.T) {
	cases := []struct {
		name    string
		trigger string
		desync  func(*testing.T, *store.Store, model.Project)
	}{
		{
			// Dropping the insert trigger and then writing content leaves the
			// index missing an entry it should have.
			name:    "missing entry",
			trigger: "issues_fts_ai",
			desync: func(t *testing.T, opened *store.Store, project model.Project) {
				seedIssue(t, opened, project, "invisible", "borogoves are mimsy")
			},
		},
		{
			// Dropping the delete trigger and then removing content leaves the
			// index holding an entry for a row that is gone.
			name:    "stale entry",
			trigger: "issues_fts_ad",
			desync: func(t *testing.T, opened *store.Store, project model.Project) {
				if _, err := opened.DB().ExecContext(t.Context(),
					`DELETE FROM issues WHERE id = 'doomed'`); err != nil {
					t.Fatalf("delete content row: %v", err)
				}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			opened := newStore(t)
			project := seedProject(t, opened, newReplicaKey(t), "drops")
			seedIssue(t, opened, project, "doomed", "brillig and slithy toves")

			if err := opened.CheckFTS(t.Context(), "issues_fts"); err != nil {
				t.Fatalf("healthy index reported a finding: %v", err)
			}

			if _, err := opened.DB().ExecContext(t.Context(),
				`DROP TRIGGER `+testCase.trigger); err != nil {
				t.Fatalf("drop %s: %v", testCase.trigger, err)
			}
			testCase.desync(t, opened, project)

			if err := opened.CheckFTS(t.Context(), "issues_fts"); err == nil {
				t.Error("rank-1 integrity check passed on a desynced index")
			}
			if err := plainFTSCheck(t, opened, "issues_fts"); err != nil {
				t.Logf("note: the plain check also caught this desync: %v", err)
			} else {
				t.Log("the plain check missed this desync, which is why rank 1 is required")
			}
		})
	}
}

// TestRebuildRepairsOnlyTheNamedIndex pins the repair contract: rebuilding fixes
// the index that failed and leaves the others alone.
func TestRebuildRepairsOnlyTheNamedIndex(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")
	seedIssue(t, opened, project, "doomed", "brillig and slithy toves")
	survivor := seedIssue(t, opened, project, "survivor", "unaffected")
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutComment(ctx, model.Comment{
			ID: "survivor:0123456789ab", IssueID: survivor.ID, Body: "outgrabe mome raths",
			CreatedAt: stampCreated, CreationReplica: project.CreationReplica,
		})
	})

	if _, err := opened.DB().ExecContext(t.Context(), `DROP TRIGGER issues_fts_ad`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if _, err := opened.DB().ExecContext(t.Context(), `DELETE FROM issues WHERE id = 'doomed'`); err != nil {
		t.Fatalf("delete content row: %v", err)
	}

	if err := opened.CheckFTS(t.Context(), "issues_fts"); err == nil {
		t.Fatal("issues_fts reported healthy after a forced desync")
	}
	if err := opened.CheckFTS(t.Context(), "comments_fts"); err != nil {
		t.Errorf("comments_fts reported a finding it should not have: %v", err)
	}

	if err := opened.RebuildFTS(t.Context(), "issues_fts"); err != nil {
		t.Fatalf("rebuild issues_fts: %v", err)
	}
	for _, index := range store.FTSIndexes() {
		if err := opened.CheckFTS(t.Context(), index); err != nil {
			t.Errorf("%s still reports a finding after the rebuild: %v", index, err)
		}
	}
}

func TestFTSOperationsRefuseAnUnknownIndex(t *testing.T) {
	opened := newStore(t)

	if err := opened.CheckFTS(t.Context(), "issues_fts; DROP TABLE issues"); err == nil {
		t.Error("CheckFTS accepted an index name that is not one of the three")
	}
	if err := opened.RebuildFTS(t.Context(), "sqlite_master"); err == nil {
		t.Error("RebuildFTS accepted an index name that is not one of the three")
	}
}

func TestQuickCheckAndForeignKeyCheckAreQuietOnAHealthyStore(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")
	seedIssue(t, opened, project, "k3f9x", "healthy")

	findings, err := opened.QuickCheck(t.Context())
	if err != nil {
		t.Fatalf("quick check: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("quick check findings on a healthy store = %v, want none", findings)
	}

	violations, err := opened.ForeignKeyCheck(t.Context())
	if err != nil {
		t.Fatalf("foreign key check: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("foreign key violations on a healthy store = %v, want none", violations)
	}
}

// TestForeignKeyCheckReportsAViolationAsRows pins that violations arrive as rows
// rather than as an error, which is what a check that only inspected Exec's
// error would miss entirely.
func TestForeignKeyCheckReportsAViolationAsRows(t *testing.T) {
	opened := newStore(t)
	project := seedProject(t, opened, newReplicaKey(t), "drops")
	seedIssue(t, opened, project, "orphan", "about to lose its Project")

	if _, err := opened.DB().ExecContext(t.Context(),
		`PRAGMA foreign_keys = OFF; DELETE FROM projects WHERE project_key = ?`,
		string(project.Key)); err != nil {
		t.Fatalf("force a dangling reference: %v", err)
	}
	t.Cleanup(func() {
		opened.DB().ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	})

	violations, err := opened.ForeignKeyCheck(t.Context())
	if err != nil {
		t.Fatalf("foreign key check: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("foreign key check found no violation after one was forced")
	}
	if violations[0].Table != "issues" {
		t.Errorf("violation table = %q, want issues", violations[0].Table)
	}
}

// plainFTSCheck runs the rank-less form, which the doctor contract rules out.
// It is here so the difference between the two forms is observed rather than
// asserted from memory.
func plainFTSCheck(t *testing.T, opened *store.Store, index string) error {
	t.Helper()
	_, err := opened.DB().ExecContext(t.Context(),
		`INSERT INTO `+index+`(`+index+`) VALUES ('integrity-check')`)
	return err
}

func TestSearchIssuesReachesTitlesDescriptionsAndComments(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")

	byTitle := newIssue(t, project, "by-title", "vorpal blade snicker-snack")
	byDescription := newIssue(t, project, "by-description", "an ordinary title")
	byDescription.Description = "the manxome foe he sought"
	byComment := newIssue(t, project, "by-comment", "another ordinary title")
	unrelated := newIssue(t, project, "unrelated", "nothing in common")

	for _, issue := range []model.Issue{byTitle, byDescription, byComment, unrelated} {
		write(t, opened, func(ctx context.Context, tx *store.Tx) error {
			if err := tx.PutIDOwner(ctx, model.IDOwner{
				ID: issue.ID, Kind: model.OwnerIssue, CreationReplica: replica,
			}); err != nil {
				return err
			}
			return tx.PutIssue(ctx, issue)
		})
	}
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutComment(ctx, model.Comment{
			ID: "by-comment:00112233aabb", IssueID: byComment.ID,
			Body: "the jabberwock with eyes of flame", CreatedAt: stampCreated,
			CreationReplica: replica,
		})
	})

	cases := map[string][]model.ID{
		"vorpal":         {byTitle.ID},
		"manxome":        {byDescription.ID},
		"jabberwock":     {byComment.ID},
		"eyes flame":     {byComment.ID},
		"vorpal manxome": {},
		"borogoves":      {},
	}
	for query, want := range cases {
		t.Run(query, func(t *testing.T) {
			found, err := opened.SearchIssues(t.Context(), query, store.IssueFilter{})
			if err != nil {
				t.Fatalf("search %q: %v", query, err)
			}
			if got := issueIDs(found); !slices.Equal(got, want) {
				t.Errorf("search %q = %v, want %v", query, got, want)
			}
		})
	}
}

// TestSearchTreatsOperatorsAsText pins that FTS5 syntax in a user's query is a
// search term, not an expression the CLI would have to explain a parse error for.
func TestSearchTreatsOperatorsAsText(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	seedIssue(t, opened, project, "operators", "NEAR and OR and NOT are words here")

	for _, query := range []string{"NEAR", "OR", `"unbalanced`, "title:something", "a^b"} {
		if _, err := opened.SearchIssues(t.Context(), query, store.IssueFilter{}); err != nil {
			t.Errorf("search %q returned an error: %v", query, err)
		}
	}

	if _, err := opened.SearchIssues(t.Context(), "   ", store.IssueFilter{}); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("search with no term: err = %v, want ErrInvalid", err)
	}
}

func TestSearchIssuesRespectsTheFilter(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	drops := seedProject(t, opened, replica, "drops")
	beacon := seedProject(t, opened, replica, "beacon")
	here := seedIssue(t, opened, drops, "here", "gyre and gimble")
	seedIssue(t, opened, beacon, "there", "gyre and gimble")

	found, err := opened.SearchIssues(t.Context(), "gyre", store.IssueFilter{Project: &drops.Key})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := issueIDs(found); !slices.Equal(got, []model.ID{here.ID}) {
		t.Errorf("scoped search = %v, want just %s", got, here.ID)
	}
}

func TestSearchMemoriesHidesSupersededAndTombstonedByDefault(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	current := seedMemory(t, opened, project, "br-et8", "current lesson", "uffish thought")
	retired := seedMemory(t, opened, project, "br-eyp", "retired lesson", "uffish thought")

	linked := retired
	linked.SupersededBy = &current.ID
	linked.Revision, _ = retired.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateMemory(ctx, linked, retired.Revision)
	})

	found, err := opened.SearchMemories(t.Context(), "uffish", store.MemoryFilter{})
	if err != nil {
		t.Fatalf("search Memories: %v", err)
	}
	if got := memoryIDs(found); !slices.Equal(got, []model.ID{current.ID}) {
		t.Errorf("search = %v, want just %s", got, current.ID)
	}

	all, err := opened.SearchMemories(t.Context(), "uffish", store.MemoryFilter{IncludeSuperseded: true})
	if err != nil {
		t.Fatalf("search Memories: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("search with history = %d Memories, want 2", len(all))
	}
}

// TestSearchFollowsAnEditRatherThanTheOriginalText proves the update triggers
// keep the indexes current: a renamed Issue stops matching its old title.
func TestSearchFollowsAnEditRatherThanTheOriginalText(t *testing.T) {
	opened := newStore(t)
	replica := newReplicaKey(t)
	project := seedProject(t, opened, replica, "drops")
	issue := seedIssue(t, opened, project, "renamed", "frumious bandersnatch")

	edited := issue
	edited.Title = "callooh callay"
	edited.Revision, _ = issue.Revision.Next(replica)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateIssue(ctx, edited, issue.Revision)
	})

	stale, err := opened.SearchIssues(t.Context(), "frumious", store.IssueFilter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("search for the old title found %v, want nothing", issueIDs(stale))
	}
	fresh, err := opened.SearchIssues(t.Context(), "callooh", store.IssueFilter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := issueIDs(fresh); !slices.Equal(got, []model.ID{issue.ID}) {
		t.Errorf("search for the new title = %v, want %s", got, issue.ID)
	}
}
