package model_test

import (
	"errors"
	"testing"

	"github.com/tedkulp/drops/internal/model"
)

func validRevision(t *testing.T) model.Revision {
	t.Helper()
	return model.Revision{
		Generation: 1,
		Replica:    mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae"),
	}
}

func validProject(t *testing.T) model.Project {
	t.Helper()
	stamp := model.Timestamp("2026-09-02T07:34:56.123Z")
	return model.Project{
		Key:             model.GlobalProjectKey,
		Slug:            model.GlobalProjectSlug,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		CreationReplica: mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae"),
		Revision:        validRevision(t),
	}
}

func validIssue(t *testing.T) model.Issue {
	t.Helper()
	stamp := model.Timestamp("2026-09-02T07:34:56.123Z")
	return model.Issue{
		ID:              model.ID("br-6vf"),
		ProjectKey:      model.GlobalProjectKey,
		Title:           "Opaque IDs survive",
		Description:     "",
		Type:            model.TypeTask,
		Status:          model.StatusOpen,
		Priority:        2,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		CreationReplica: mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae"),
		Revision:        validRevision(t),
	}
}

func validMemory(t *testing.T) model.Memory {
	t.Helper()
	stamp := model.Timestamp("2026-09-02T07:34:56.123Z")
	return model.Memory{
		ID:              model.ID("mem-old-shape"),
		ProjectKey:      model.GlobalProjectKey,
		Title:           "A durable note",
		Body:            "The body remains searchable.",
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		CreationReplica: mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae"),
		Revision:        validRevision(t),
	}
}

func TestIssueVocabulariesAreClosed(t *testing.T) {
	t.Parallel()

	for _, issueType := range []model.IssueType{
		model.TypeTask,
		model.TypeBug,
		model.TypeFeature,
		model.TypeEpic,
		model.TypeChore,
		model.TypeResearch,
		model.TypeDecision,
	} {
		if !model.ValidIssueType(issueType) {
			t.Errorf("retained Issue type %q is invalid", issueType)
		}
	}
	if model.ValidIssueType(model.IssueType("story")) {
		t.Error("unknown Issue type is valid")
	}

	for _, status := range []model.Status{model.StatusOpen, model.StatusInProgress, model.StatusClosed} {
		if !model.ValidStatus(status) {
			t.Errorf("retained status %q is invalid", status)
		}
	}
	for _, cut := range []model.Status{"blocked", "deleted"} {
		if model.ValidStatus(cut) {
			t.Errorf("cut status %q remains valid", cut)
		}
	}
	if !model.StatusClosed.IsTerminal() || model.StatusOpen.IsTerminal() || model.StatusInProgress.IsTerminal() {
		t.Error("terminal status classification is wrong")
	}

	for _, dependencyType := range []model.DependencyType{model.DepBlocks, model.DepRelated, model.DepDiscoveredFrom} {
		if !model.ValidDependencyType(dependencyType) {
			t.Errorf("retained dependency type %q is invalid", dependencyType)
		}
	}
	if model.ValidDependencyType(model.DependencyType("parent-child")) {
		t.Error("parent-child remains a dependency type")
	}

	for priority := 0; priority <= 4; priority++ {
		if !model.ValidPriority(priority) {
			t.Errorf("priority %d is invalid", priority)
		}
	}
	if model.ValidPriority(-1) || model.ValidPriority(5) {
		t.Error("priority outside 0..4 is valid")
	}
}

func TestCoreRecordsValidateTheirStorageInvariants(t *testing.T) {
	t.Parallel()

	project := validProject(t)
	if err := project.Validate(); err != nil {
		t.Fatalf("valid Project: %v", err)
	}

	issue := validIssue(t)
	if err := issue.Validate(); err != nil {
		t.Fatalf("valid Issue: %v", err)
	}
	issue.Status = model.StatusClosed
	issue.ClosedAt = nil
	if err := issue.Validate(); err != nil {
		t.Fatalf("closed Issue without closed_at must remain representable: %v", err)
	}
	issue.Tombstone = model.Tombstoned
	issue.Status = model.StatusOpen
	if err := issue.Validate(); err != nil {
		t.Fatalf("tombstoned open Issue must remain representable: %v", err)
	}

	memory := validMemory(t)
	if err := memory.Validate(); err != nil {
		t.Fatalf("valid Memory: %v", err)
	}
	memory.Tombstone = model.Tombstoned
	if err := memory.Validate(); err != nil {
		t.Fatalf("tombstoned Memory: %v", err)
	}
}

func TestCoreRecordsRejectMalformedValues(t *testing.T) {
	t.Parallel()

	project := validProject(t)
	project.Slug = ""
	assertInvalid(t, "empty Project slug", project.Validate())

	issue := validIssue(t)
	issue.Title = ""
	assertInvalid(t, "empty Issue title", issue.Validate())
	issue = validIssue(t)
	issue.Type = model.IssueType("story")
	assertInvalid(t, "unknown Issue type", issue.Validate())
	issue = validIssue(t)
	issue.Status = model.Status("blocked")
	assertInvalid(t, "cut Issue status", issue.Validate())
	issue = validIssue(t)
	issue.Priority = 5
	assertInvalid(t, "high priority", issue.Validate())

	memory := validMemory(t)
	memory.Body = ""
	assertInvalid(t, "empty Memory body", memory.Validate())
	memory = validMemory(t)
	memory.SupersededBy = new(memory.ID)
	assertInvalid(t, "self-superseding Memory", memory.Validate())
}

func TestRelationshipRecordsValidateIndependentLiveness(t *testing.T) {
	t.Parallel()

	stamp := model.Timestamp("2026-09-02T07:34:56.123Z")
	revision := validRevision(t)
	replica := revision.Replica

	records := []interface{ Validate() error }{
		model.IDOwner{ID: "br-6vf", Kind: model.OwnerIssue, CreationReplica: replica},
		model.RepositoryLocator{ProjectKey: model.GlobalProjectKey, Locator: "github.com/tedkulp/drops", Revision: revision},
		model.WorkspaceBinding{Path: "/home/ted/src/drops", ProjectKey: model.GlobalProjectKey},
		model.IssueParent{ChildID: "br-6vf.1", ParentID: "br-6vf", CreatedAt: stamp, Tombstone: model.Tombstoned, Revision: revision},
		model.Dependency{FromID: "br-6vf.1", ToID: "br-6vf", Type: model.DepBlocks, CreatedAt: stamp, Revision: revision},
		model.Label{IssueID: "br-6vf", Name: "wayfinder:map", Revision: revision},
		model.Comment{ID: "br-6vf:acd91ca2f7b0", IssueID: "br-6vf", Author: "", Body: "", CreatedAt: stamp, CreationReplica: replica},
	}
	for _, record := range records {
		if err := record.Validate(); err != nil {
			t.Errorf("%T.Validate: %v", record, err)
		}
	}

	assertInvalid(t, "self parent", (model.IssueParent{ChildID: "same", ParentID: "same", CreatedAt: stamp, Revision: revision}).Validate())
	assertInvalid(t, "self dependency", (model.Dependency{FromID: "same", ToID: "same", Type: model.DepBlocks, CreatedAt: stamp, Revision: revision}).Validate())
	assertInvalid(t, "empty label", (model.Label{IssueID: "br-6vf", Revision: revision}).Validate())
}

func TestReplicaStateHasSeparateIdentityAndSuccession(t *testing.T) {
	t.Parallel()

	oldKey := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaae")
	newKey := mustReplicaKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaai")
	stamp := model.Timestamp("2026-09-02T07:34:56.123Z")

	for _, record := range []interface{ Validate() error }{
		model.Replica{Key: oldKey},
		model.ReplicaHead{Replica: oldKey, SnapshotHead: "abc123"},
		model.ReplicaSuccession{OldReplica: oldKey, NewReplica: newKey, RekeyedAt: stamp},
	} {
		if err := record.Validate(); err != nil {
			t.Errorf("%T.Validate: %v", record, err)
		}
	}

	assertInvalid(t, "self succession", (model.ReplicaSuccession{OldReplica: oldKey, NewReplica: oldKey, RekeyedAt: stamp}).Validate())
	assertInvalid(t, "empty snapshot head", (model.ReplicaHead{Replica: oldKey}).Validate())
}

func assertInvalid(t *testing.T, name string, err error) {
	t.Helper()
	if !errors.Is(err, model.ErrInvalid) {
		t.Errorf("%s error = %v, want ErrInvalid", name, err)
	}
}
