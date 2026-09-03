package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

// twoProjects seeds a Project to move out of and a Project to move into.
func twoProjects(t *testing.T, rules *core.Core) (model.Project, model.Project) {
	t.Helper()
	from, err := rules.CreateProject(t.Context(), "here")
	if err != nil {
		t.Fatalf("create Project here: %v", err)
	}
	to, err := rules.CreateProject(t.Context(), "there")
	if err != nil {
		t.Fatalf("create Project there: %v", err)
	}
	return from, to
}

// The clause: `drops move` refiles every named Issue in ONE transaction, so a
// list whose second entry is unmovable moves none of them. This is the crossing
// report docs/agents/issue-tracker.md documents; a per-Issue commit would leave
// a subtree half-refiled with no way to name what happened.
func TestMoveIssuesCommitsAllOrNothing(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "movable")
	from, to := twoProjects(t, rules)
	if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: from.Key, Title: "movable", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create Issue: %v", err)
	}

	_, err := rules.MoveIssues(t.Context(), []model.ID{"movable", "nobody"}, to.Key)
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("move error = %v, want ErrNotFound", err)
	}
	stayed, err := rules.Issue(t.Context(), "movable")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stayed.ProjectKey != from.Key {
		t.Fatalf("Issue moved anyway: project = %s, want %s", stayed.ProjectKey, from.Key)
	}
}

// The clause: an Issue already in the destination is left alone rather than
// rewritten. A no-op move that still bumped the revision would export as a
// concurrent edit on the next sync, manufacturing a conflict out of nothing.
func TestMoveIssuesSkipsAnIssueAlreadyInTheDestination(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "settled", "arriving")
	from, to := twoProjects(t, rules)
	settled, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: to.Key, Title: "settled", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create settled Issue: %v", err)
	}
	if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: from.Key, Title: "arriving", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create arriving Issue: %v", err)
	}

	moved, err := rules.MoveIssues(t.Context(), []model.ID{"settled", "arriving"}, to.Key)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(moved) != 1 || moved[0].ID != "arriving" {
		t.Fatalf("moved = %#v, want only the Issue that changed Project", moved)
	}
	unchanged, err := rules.Issue(t.Context(), settled.ID)
	if err != nil {
		t.Fatalf("read settled: %v", err)
	}
	if unchanged.Revision != settled.Revision {
		t.Fatalf("settled revision = %#v, want %#v — a no-op move rewrote it", unchanged.Revision, settled.Revision)
	}
}

// The clause: an archived Project does not accept new work, so a move into one
// is invalid input rather than a silent refile into a dead Project.
func TestMoveIssuesRefusesAnArchivedDestination(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "movable")
	from, to := twoProjects(t, rules)
	if _, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: from.Key, Title: "movable", Type: model.TypeTask, Priority: 2,
	}); err != nil {
		t.Fatalf("create Issue: %v", err)
	}
	if _, err := rules.ArchiveProject(t.Context(), to.Key); err != nil {
		t.Fatalf("archive destination: %v", err)
	}

	if _, err := rules.MoveIssues(t.Context(), []model.ID{"movable"}, to.Key); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("move into archived Project error = %v, want ErrInvalid", err)
	}
	stayed, err := rules.Issue(t.Context(), "movable")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stayed.ProjectKey != from.Key {
		t.Fatalf("Issue moved into an archived Project: project = %s", stayed.ProjectKey)
	}
}

// The clause: a Comment does NOT stamp its Issue's updated_at.
//
// docs/agents/issue-tracker.md states this outright, because a parser that
// polls updated_at to find changed Issues would otherwise be told wrong. It is
// deliberate: a Comment is its own replicated record with its own identity, so
// the Issue has not changed when one arrives.
func TestAddCommentLeavesTheIssueUpdatedAtAlone(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "commented")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	issue, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "commented", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}

	comment, err := rules.AddComment(t.Context(), issue.ID, "ted", "a remark that changes no Issue")
	if err != nil {
		t.Fatalf("add Comment: %v", err)
	}
	after, err := rules.Issue(t.Context(), issue.ID)
	if err != nil {
		t.Fatalf("read back Issue: %v", err)
	}
	if after.UpdatedAt != issue.UpdatedAt {
		t.Errorf("Issue updated_at = %s, want %s unchanged by a Comment", after.UpdatedAt, issue.UpdatedAt)
	}
	if after.Revision != issue.Revision {
		t.Errorf("Issue revision = %#v, want %#v unchanged by a Comment", after.Revision, issue.Revision)
	}

	thread, err := rules.IssueComments(t.Context(), issue.ID)
	if err != nil || len(thread) != 1 || thread[0].ID != comment.ID {
		t.Fatalf("thread = %#v, %v", thread, err)
	}
	if !strings.HasPrefix(string(comment.ID), string(issue.ID)+":") {
		t.Errorf("Comment id %q is not derived from its Issue", comment.ID)
	}
}

// The clause: an empty Comment body is invalid input, refused before any write.
func TestAddCommentRefusesAnEmptyBody(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "commented")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	issue, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "commented", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}

	if _, err := rules.AddComment(t.Context(), issue.ID, "ted", ""); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("empty Comment error = %v, want ErrInvalid", err)
	}
	thread, err := rules.IssueComments(t.Context(), issue.ID)
	if err != nil || len(thread) != 0 {
		t.Fatalf("thread = %#v, %v, want empty", thread, err)
	}
}

// The clause: a Comment on an Issue that does not exist is refused by NAMING
// the Issue, not by letting SQLite's foreign key catch it.
//
// ErrNotFound alone cannot carry this clause: internal/store maps constraint
// code 787 to ErrNotFound too, so removing the explicit read still produces an
// ErrNotFound — one reading "constraint failed: FOREIGN KEY constraint failed
// (787)", which names neither the Issue nor the rule. This is the same defect
// dw32p.27 found in `memory edit`, so the assertion is on what the agent reads.
func TestAddCommentRefusesAnUnknownIssueByName(t *testing.T) {
	rules, _ := openCore(t)
	_, err := rules.AddComment(t.Context(), "nobody", "ted", "into the void")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Comment on unknown Issue error = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Errorf("refusal does not name the Issue: %v", err)
	}
	if strings.Contains(err.Error(), "constraint") {
		t.Errorf("refusal leaks SQLite's constraint text: %v", err)
	}
}

// The clause: a Comment body is scanned for credentials like every other
// authored prose field, and the warning names the Comment rather than the Issue.
func TestAddCommentWarnsOnACredentialWithoutRefusingIt(t *testing.T) {
	_, opened := openCore(t)
	replica, _ := model.ParseReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae")
	var warnings []core.Warning
	rules := core.New(opened, replica, fixedClock{now: fixedNow}, &sequenceIDs{values: []model.ID{"commented"}},
		func(warning core.Warning) { warnings = append(warnings, warning) })
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	issue, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "commented", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}

	comment, err := rules.AddComment(t.Context(), issue.ID, "ted", "the token is ghp_123456789012345678901234567890123456")
	if err != nil {
		t.Fatalf("add Comment: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %#v, want exactly one", warnings)
	}
	if warnings[0].Entity.Kind != model.RecordComment || warnings[0].Entity.Key != string(comment.ID) {
		t.Errorf("warning names %#v, want the Comment %s", warnings[0].Entity, comment.ID)
	}
	if warnings[0].Field != "body" {
		t.Errorf("warning field = %q, want body", warnings[0].Field)
	}
	thread, err := rules.IssueComments(t.Context(), issue.ID)
	if err != nil || len(thread) != 1 {
		t.Fatalf("a warning must not refuse the write: thread = %#v, %v", thread, err)
	}
}

// The clause: DeleteComment removes the Comment outright. A Comment is the one
// record with no revision of its own, so there is no tombstone to set.
func TestDeleteCommentRemovesItFromTheThread(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "commented")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	issue, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "commented", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create Issue: %v", err)
	}
	comment, err := rules.AddComment(t.Context(), issue.ID, "ted", "a remark to retract")
	if err != nil {
		t.Fatalf("add Comment: %v", err)
	}

	if err := rules.DeleteComment(t.Context(), comment.ID); err != nil {
		t.Fatalf("delete Comment: %v", err)
	}
	thread, err := rules.IssueComments(t.Context(), issue.ID)
	if err != nil || len(thread) != 0 {
		t.Fatalf("thread = %#v, %v, want empty", thread, err)
	}
}

// The clause: forgetting a Memory tombstones it and bumps the revision, and
// forgetting an already-forgotten one is a no-op rather than a second bump.
// The idempotence is what keeps a repeated `forget` from exporting as an edit.
func TestSetMemoryTombstoneIsIdempotent(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "mem-one")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	created, err := rules.CreateMemory(t.Context(), core.CreateMemory{
		Project: project.Key, Title: "one", Body: "a note worth forgetting",
	})
	if err != nil {
		t.Fatalf("create Memory: %v", err)
	}

	forgotten, err := rules.SetMemoryTombstone(t.Context(), created.ID, model.Tombstoned)
	if err != nil {
		t.Fatalf("forget Memory: %v", err)
	}
	if forgotten.Tombstone != model.Tombstoned ||
		forgotten.Revision.Generation != created.Revision.Generation+1 {
		t.Fatalf("forgotten = %#v, want tombstoned at generation %d",
			forgotten, created.Revision.Generation+1)
	}
	again, err := rules.SetMemoryTombstone(t.Context(), created.ID, model.Tombstoned)
	if err != nil {
		t.Fatalf("forget again: %v", err)
	}
	if again.Revision != forgotten.Revision {
		t.Fatalf("second forget = %#v, want the revision left at %#v", again.Revision, forgotten.Revision)
	}

	restored, err := rules.SetMemoryTombstone(t.Context(), created.ID, model.Live)
	if err != nil {
		t.Fatalf("restore Memory: %v", err)
	}
	if restored.Tombstone != model.Live ||
		restored.Revision.Generation != forgotten.Revision.Generation+1 {
		t.Fatalf("restored = %#v, want live at generation %d",
			restored, forgotten.Revision.Generation+1)
	}
}

// The clause (dw32p.9): a supersession chain is ONE-TO-ONE. A Memory that has
// already been superseded, or forgotten, cannot be superseded again — two
// replacements for one link would make "what replaced this" unanswerable.
func TestSupersedeMemoryRefusesARetiredMemory(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "mem-first", "mem-second", "mem-third", "mem-gone", "mem-after")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}
	first, err := rules.CreateMemory(t.Context(), core.CreateMemory{
		Project: project.Key, Title: "first", Body: "the first take",
	})
	if err != nil {
		t.Fatalf("create Memory: %v", err)
	}
	if _, err := rules.SupersedeMemory(t.Context(), first.ID, core.CreateMemory{
		Title: "second", Body: "the corrected take",
	}); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	// Already superseded.
	if _, err := rules.SupersedeMemory(t.Context(), first.ID, core.CreateMemory{
		Title: "third", Body: "a second replacement for one link",
	}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("superseding an already-superseded Memory = %v, want ErrConflict", err)
	}

	// Already forgotten.
	gone, err := rules.CreateMemory(t.Context(), core.CreateMemory{
		Project: project.Key, Title: "gone", Body: "a note about to be forgotten",
	})
	if err != nil {
		t.Fatalf("create Memory: %v", err)
	}
	if _, err := rules.SetMemoryTombstone(t.Context(), gone.ID, model.Tombstoned); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if _, err := rules.SupersedeMemory(t.Context(), gone.ID, core.CreateMemory{
		Title: "after", Body: "a replacement for something forgotten",
	}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("superseding a forgotten Memory = %v, want ErrConflict", err)
	}

	// The one legitimate replacement is still the only one.
	chain, err := rules.Memory(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if chain.SupersededBy == nil || *chain.SupersededBy != "mem-second" {
		t.Fatalf("chain = %#v, want it still pointing at the one replacement", chain)
	}
}

// The clause (dw32p.7): an Issue with no title, an unknown type or an
// out-of-range priority is invalid input, refused before an ID is reserved —
// an ID spent on a rejected Issue is spent permanently.
func TestCreateIssueRefusesInvalidFields(t *testing.T) {
	rules, _ := openCoreWithIDs(t, "never-reserved")
	project, err := rules.CreateProject(t.Context(), "drops")
	if err != nil {
		t.Fatalf("create Project: %v", err)
	}

	for _, testcase := range []struct {
		name  string
		input core.CreateIssue
	}{
		{"no title", core.CreateIssue{Project: project.Key, Type: model.TypeTask, Priority: 2}},
		{"unknown type", core.CreateIssue{Project: project.Key, Title: "t", Type: model.IssueType("saga"), Priority: 2}},
		{"priority out of range", core.CreateIssue{Project: project.Key, Title: "t", Type: model.TypeTask, Priority: 9}},
		{"empty label", core.CreateIssue{Project: project.Key, Title: "t", Type: model.TypeTask, Priority: 2, Labels: []string{""}}},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			if _, err := rules.CreateIssue(t.Context(), testcase.input); !errors.Is(err, model.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}

	// The pinned ID source held exactly one value; none of the four refusals
	// drew it, so a rejected Issue costs no identity.
	created, err := rules.CreateIssue(t.Context(), core.CreateIssue{
		Project: project.Key, Title: "valid", Type: model.TypeTask, Priority: 2,
	})
	if err != nil {
		t.Fatalf("create valid Issue: %v", err)
	}
	if created.ID != "never-reserved" {
		t.Fatalf("Issue id = %s, want the first pinned id — a refusal consumed one", created.ID)
	}
}
