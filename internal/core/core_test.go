package core_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type sequenceIDs struct {
	values []model.ID
}

func (source *sequenceIDs) NewID(model.OwnerKind, *model.ID, []model.ID) (model.ID, error) {
	if len(source.values) == 0 {
		return "", errors.New("ID sequence exhausted")
	}
	id := source.values[0]
	source.values = source.values[1:]
	return id, nil
}

func openCore(t *testing.T) (*core.Core, *store.Store) {
	t.Helper()
	opened, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "drops.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	replica, err := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatalf("parse replica: %v", err)
	}
	clock := fixedClock{now: time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC)}
	return core.New(opened, replica, clock, nil, nil), opened
}

func TestProjectLifecyclePreservesIdentityAndReservedRules(t *testing.T) {
	rules, _ := openCore(t)

	created, err := rules.CreateProject(t.Context(), "beacon")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	renamed, err := rules.RenameProject(t.Context(), created.Key, "drops")
	if err != nil {
		t.Fatalf("rename Project: %v", err)
	}
	if renamed.Key != created.Key || renamed.Slug != "drops" || renamed.Revision.Generation != 2 {
		t.Fatalf("rename = %#v, want same key, drops slug, generation 2", renamed)
	}
	archived, err := rules.ArchiveProject(t.Context(), created.Key)
	if err != nil {
		t.Fatalf("archive Project: %v", err)
	}
	if archived.ArchivedAt == nil || archived.Revision.Generation != 3 {
		t.Fatalf("archive = %#v, want archived generation 3", archived)
	}
	if _, err := rules.RenameProject(t.Context(), model.GlobalProjectKey, "elsewhere"); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("rename reserved Project error = %v, want ErrInvalid", err)
	}
	if _, err := rules.CreateProject(t.Context(), "drops"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("duplicate slug error = %v, want ErrConflict", err)
	}
}

func TestCreateIssueReservesIDAndRelationsInOneTransaction(t *testing.T) {
	_, opened := openCore(t)
	replica, _ := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	rules := core.New(
		opened,
		replica,
		fixedClock{now: time.Date(2026, 9, 2, 16, 0, 0, 0, time.UTC)},
		&sequenceIDs{values: []model.ID{"k3f9x", "bad-one"}},
		nil,
	)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}

	created, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project:     project.Key,
		Title:       "transactional Issue",
		Description: "one unit",
		Type:        model.TypeTask,
		Priority:    2,
		Labels:      []string{"wayfinder:task", "core"},
	})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}
	owner, err := opened.IDOwner(t.Context(), created.ID)
	if err != nil || owner.Kind != model.OwnerIssue {
		t.Fatalf("ID owner = %#v, %v", owner, err)
	}
	labels, err := opened.IssueLabels(t.Context(), created.ID)
	if err != nil || len(labels) != 2 {
		t.Fatalf("labels = %#v, %v", labels, err)
	}

	invalidStamp := model.Timestamp("not-a-time")
	_, err = rules.CreateIssue(t.Context(), core.CreateIssue{
		Project:       project.Key,
		Title:         "must roll back",
		Type:          model.TypeTask,
		Priority:      2,
		DeferredUntil: &invalidStamp,
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("invalid Issue error = %v, want ErrInvalid", err)
	}
	if _, err := opened.IDOwner(t.Context(), "bad-one"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("rolled-back reservation error = %v, want ErrNotFound", err)
	}
}

func TestSupersedeMemoryIsAtomicAndWarnsWithoutLeakingSecret(t *testing.T) {
	_, opened := openCore(t)
	replica, _ := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	var warnings []core.Warning
	rules := core.New(
		opened,
		replica,
		fixedClock{now: time.Date(2026, 9, 2, 17, 0, 0, 0, time.UTC)},
		&sequenceIDs{values: []model.ID{"mem-first", "mem-next", "mem-doomed", "mem-leak"}},
		func(warning core.Warning) { warnings = append(warnings, warning) },
	)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	first, err := rules.CreateMemory(t.Context(), core.CreateMemory{
		Project: project.Key,
		Title:   "credential note",
		Body:    "token ghp_123456789012345678901234567890123456",
	})
	if err != nil {
		t.Fatalf("create Memory: %v", err)
	}
	if len(warnings) != 1 || warnings[0].Family != "GitHub personal access token" || warnings[0].Field != "body" {
		t.Fatalf("warnings = %#v", warnings)
	}

	replacement, err := rules.SupersedeMemory(t.Context(), first.ID, core.CreateMemory{
		Title: "corrected note",
		Body:  "safe replacement",
	})
	if err != nil {
		t.Fatalf("supersede Memory: %v", err)
	}
	old, err := opened.Memory(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("read old Memory: %v", err)
	}
	if old.SupersededBy == nil || *old.SupersededBy != replacement.ID || old.Revision.Generation != 2 {
		t.Fatalf("old Memory = %#v", old)
	}
	if replacement.ProjectKey != first.ProjectKey || replacement.CreatedAt <= first.CreatedAt {
		t.Fatalf("replacement = %#v, first = %#v", replacement, first)
	}
	listed, err := opened.Memories(t.Context(), store.MemoryFilter{})
	if err != nil || len(listed) != 1 || listed[0].ID != replacement.ID {
		t.Fatalf("ordinary Memories = %#v, %v", listed, err)
	}

	doomed, err := rules.CreateMemory(t.Context(), core.CreateMemory{
		Project: project.Key, Title: "doomed", Body: "must remain",
	})
	if err != nil {
		t.Fatalf("create doomed Memory: %v", err)
	}
	observed := doomed.Revision
	doomed.Revision.Generation = math.MaxInt64
	if err := opened.WithTx(t.Context(), func(ctx context.Context, tx *store.Tx) error {
		return tx.UpdateMemory(ctx, doomed, observed)
	}); err != nil {
		t.Fatalf("seed exhausted revision: %v", err)
	}
	if _, err := rules.SupersedeMemory(t.Context(), doomed.ID, core.CreateMemory{
		Title: "cannot commit", Body: "replacement",
	}); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("overflowing supersession error = %v, want ErrInvalid", err)
	}
	if _, err := opened.IDOwner(t.Context(), "mem-leak"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("rolled-back replacement owner error = %v, want ErrNotFound", err)
	}
}

func TestImportRejectsIdentityCollisionWithoutPartialCommit(t *testing.T) {
	_, sourceStore := openCore(t)
	sourceReplica, _ := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	source := core.New(
		sourceStore,
		sourceReplica,
		fixedClock{now: time.Date(2026, 9, 2, 18, 0, 0, 0, time.UTC)},
		&sequenceIDs{values: []model.ID{"shared-id"}},
		nil,
	)
	sourceProject, err := source.CreateProject(t.Context(), "source")
	if err != nil {
		t.Fatalf("create source Project: %v", err)
	}
	if _, err := source.CreateIssue(t.Context(), core.CreateIssue{
		Project: sourceProject.Key, Title: "from source", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create source Issue: %v", err)
	}
	snapshot, err := source.Export(t.Context(), nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	_, targetStore := openCore(t)
	replica, _ := model.ParseReplicaKey("bbbbbbbbbbbbbbbbbbbbbbbbbe")
	target := core.New(
		targetStore,
		replica,
		fixedClock{now: time.Date(2026, 9, 2, 18, 0, 0, 0, time.UTC)},
		&sequenceIDs{values: []model.ID{"shared-id"}},
		nil,
	)
	targetProject, err := target.CreateProject(t.Context(), "target")
	if err != nil {
		t.Fatalf("create target Project: %v", err)
	}
	if _, err := target.CreateIssue(t.Context(), core.CreateIssue{
		Project: targetProject.Key, Title: "from target", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create target Issue: %v", err)
	}

	if _, err := target.Import(t.Context(), snapshot); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("import error = %v, want ErrConflict", err)
	}
	if _, err := targetStore.Project(t.Context(), sourceProject.Key); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("partially imported Project error = %v, want ErrNotFound", err)
	}
}

func TestIssueLifecycleAndRelationsAdvanceRevisionedState(t *testing.T) {
	_, opened := openCore(t)
	replica, _ := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	rules := core.New(
		opened,
		replica,
		fixedClock{now: time.Date(2026, 9, 2, 19, 0, 0, 0, time.UTC)},
		&sequenceIDs{values: []model.ID{"parent", "child"}},
		nil,
	)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	parent, err := rules.CreateIssue(t.Context(), core.CreateIssue{Project: project.Key, Title: "parent", Type: model.TypeEpic, Priority: 2})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := rules.CreateIssue(t.Context(), core.CreateIssue{Project: project.Key, Title: "child", Type: model.TypeTask, Priority: 2})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	started, err := rules.SetIssueStatus(t.Context(), child.ID, model.StatusInProgress, "")
	if err != nil || started.StartedAt == nil {
		t.Fatalf("start Issue = %#v, %v", started, err)
	}
	closed, err := rules.SetIssueStatus(t.Context(), child.ID, model.StatusClosed, "done")
	if err != nil || closed.ClosedAt == nil || closed.CloseReason == nil || *closed.CloseReason != "done" {
		t.Fatalf("close Issue = %#v, %v", closed, err)
	}
	if _, err := rules.SetIssueParent(t.Context(), child.ID, &parent.ID); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	label, err := rules.SetLabel(t.Context(), child.ID, "core", true)
	if err != nil {
		t.Fatalf("add label: %v", err)
	}
	removed, err := rules.SetLabel(t.Context(), child.ID, "core", false)
	if err != nil || removed.Tombstone != model.Tombstoned || removed.Revision.Generation != label.Revision.Generation+1 {
		t.Fatalf("removed label = %#v, %v", removed, err)
	}
	dependency, err := rules.SetDependency(t.Context(), child.ID, parent.ID, model.DepBlocks, true)
	if err != nil || dependency.Tombstone != model.Live {
		t.Fatalf("add dependency = %#v, %v", dependency, err)
	}
	if _, err := rules.SetIssueStatus(t.Context(), child.ID, model.StatusOpen, ""); err != nil {
		t.Fatalf("reopen child: %v", err)
	}
	filter := core.IssueFilter{Project: &project.Key}
	blocked, err := rules.Blocked(t.Context(), filter)
	if err != nil || len(blocked) != 1 || blocked[0].Issue.ID != child.ID ||
		len(blocked[0].BlockedBy) != 1 || blocked[0].BlockedBy[0] != parent.ID {
		t.Fatalf("blocked = %#v, %v", blocked, err)
	}
	if _, err := rules.SetIssueStatus(t.Context(), parent.ID, model.StatusClosed, "unblocked"); err != nil {
		t.Fatalf("close parent: %v", err)
	}
	ready, err := rules.Ready(t.Context(), filter)
	if err != nil || len(ready) != 1 || ready[0].ID != child.ID {
		t.Fatalf("ready = %#v, %v", ready, err)
	}
}

func TestDoctorRepairsOnlyFailedFTSAndScansRetainedProse(t *testing.T) {
	rules, opened := openCore(t)
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	issue, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "safe", Description: "ghp_123456789012345678901234567890123456",
		Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}
	if _, err := opened.DB().ExecContext(t.Context(), "DROP TRIGGER issues_fts_au"); err != nil {
		t.Fatalf("drop FTS update trigger: %v", err)
	}
	changed := "desynchronized"
	if _, err := rules.EditIssue(t.Context(), issue.ID, core.IssueEdit{Title: &changed}); err != nil {
		t.Fatalf("edit Issue: %v", err)
	}
	if _, err := rules.SetIssueTombstone(t.Context(), issue.ID, model.Tombstoned); err != nil {
		t.Fatalf("tombstone Issue: %v", err)
	}

	report := rules.Doctor(t.Context(), core.DoctorOptions{Repair: true, Scan: true})
	if len(report.AttemptedRepairs) != 1 || report.AttemptedRepairs[0] != "issues_fts" {
		t.Fatalf("attempted repairs = %#v", report.AttemptedRepairs)
	}
	if len(report.Repaired) != 1 || report.Repaired[0] != "issues_fts" {
		t.Fatalf("doctor report = %#v", report)
	}
	if report.Healthy() {
		t.Fatal("credential finding reported healthy")
	}
	if report.CredentialRowsScanned != 1 || len(report.Credentials) != 1 ||
		report.Credentials[0].Entity.Key != string(issue.ID) ||
		report.Credentials[0].Field != "description" {
		t.Fatalf("credential scan = %d rows, %#v", report.CredentialRowsScanned, report.Credentials)
	}
}

func TestImportUsesDeleteWinsForConcurrentRevisions(t *testing.T) {
	source, _ := openCore(t)
	project, err := source.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create source Project: %v", err)
	}
	issue, err := source.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "original", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create source Issue: %v", err)
	}
	snapshot, err := source.Export(t.Context(), nil)
	if err != nil {
		t.Fatalf("export source: %v", err)
	}

	_, targetStore := openCore(t)
	targetReplica, _ := model.ParseReplicaKey("bbbbbbbbbbbbbbbbbbbbbbbbbe")
	target := core.New(targetStore, targetReplica, fixedClock{now: time.Date(2026, 9, 2, 20, 0, 0, 0, time.UTC)}, nil, nil)
	if _, err := target.Import(t.Context(), snapshot); err != nil {
		t.Fatalf("initial import: %v", err)
	}
	if _, err := target.SetIssueTombstone(t.Context(), issue.ID, model.Tombstoned); err != nil {
		t.Fatalf("delete target Issue: %v", err)
	}

	incoming := issue
	incoming.Title = "concurrent live edit"
	incomingReplica, _ := model.ParseReplicaKey("ccccccccccccccccccccccccce")
	incoming.Revision = model.Revision{Generation: 2, Replica: incomingReplica}
	record, err := mirror.NewRecord(incoming)
	if err != nil {
		t.Fatalf("wrap incoming Issue: %v", err)
	}
	report, err := target.Import(t.Context(), mirror.Snapshot{Records: []mirror.Record{record}})
	if err != nil {
		t.Fatalf("merge concurrent Issue: %v", err)
	}
	if len(report.Conflicts) != 1 || !report.Conflicts[0].DeleteWins ||
		report.Conflicts[0].Chosen.Tombstone != model.Tombstoned {
		t.Fatalf("merge report = %#v", report)
	}
	got, err := target.Issue(t.Context(), issue.ID)
	if err != nil || got.Tombstone != model.Tombstoned || got.Title != issue.Title {
		t.Fatalf("merged Issue = %#v, %v", got, err)
	}
}

func TestRegistrationRollsBackProjectAndLocatorWhenBindingFails(t *testing.T) {
	rules, opened := openCore(t)
	if _, err := opened.DB().ExecContext(t.Context(), `
		CREATE TRIGGER reject_binding BEFORE INSERT ON workspace_bindings
		BEGIN SELECT RAISE(ABORT, 'reject binding'); END;
	`); err != nil {
		t.Fatalf("install binding failure: %v", err)
	}
	_, err := rules.RegisterProject(t.Context(), core.Registration{
		Slug: "atomic", Locator: "example.com/atomic", BindingPath: "/workspace/atomic",
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("registration error = %v, want ErrInvalid", err)
	}
	if _, err := opened.ProjectBySlug(t.Context(), "atomic"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("rolled-back Project error = %v, want ErrNotFound", err)
	}
	locators, err := opened.ProjectsByLocator(t.Context(), "example.com/atomic")
	if err != nil || len(locators) != 0 {
		t.Fatalf("rolled-back locators = %#v, %v", locators, err)
	}
}

func TestImportRejectsParentCycleAndCommitsNothing(t *testing.T) {
	_, sourceStore := openCore(t)
	replica, _ := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	source := core.New(
		sourceStore, replica,
		fixedClock{now: time.Date(2026, 9, 2, 21, 0, 0, 0, time.UTC)},
		&sequenceIDs{values: []model.ID{"first", "second"}}, nil,
	)
	project, err := source.CreateProject(t.Context(), "source")
	if err != nil {
		t.Fatalf("create source Project: %v", err)
	}
	first, err := source.CreateIssue(t.Context(), core.CreateIssue{Project: project.Key, Title: "first", Type: model.TypeTask, Priority: 2})
	if err != nil {
		t.Fatalf("create first Issue: %v", err)
	}
	second, err := source.CreateIssue(t.Context(), core.CreateIssue{Project: project.Key, Title: "second", Type: model.TypeTask, Priority: 2})
	if err != nil {
		t.Fatalf("create second Issue: %v", err)
	}
	snapshot, err := source.Export(t.Context(), nil)
	if err != nil {
		t.Fatalf("export source: %v", err)
	}
	for _, relation := range []model.IssueParent{
		{ChildID: first.ID, ParentID: second.ID, CreatedAt: first.CreatedAt, Revision: first.Revision},
		{ChildID: second.ID, ParentID: first.ID, CreatedAt: second.CreatedAt, Revision: second.Revision},
	} {
		record, err := mirror.NewRecord(relation)
		if err != nil {
			t.Fatalf("wrap parent: %v", err)
		}
		snapshot.Records = append(snapshot.Records, record)
	}

	target, targetStore := openCore(t)
	if _, err := target.Import(t.Context(), snapshot); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("cycle import error = %v, want ErrInvalid", err)
	}
	if _, err := targetStore.Project(t.Context(), project.Key); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("partially imported Project error = %v, want ErrNotFound", err)
	}
}
