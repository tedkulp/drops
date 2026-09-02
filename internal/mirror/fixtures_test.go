package mirror_test

import (
	"github.com/tedkulp/drops/internal/model"
)

// Fixed, valid 128-bit Base32 keys. The reserved project keys are ordinary
// valid keys and double as deterministic replica keys in tests.
var (
	replicaA = model.ReplicaKey(model.GlobalProjectKey)
	replicaB = model.ReplicaKey(model.InboxProjectKey)
	stamp    = model.Timestamp("2026-09-02T07:34:56.123Z")
)

func validRevision() model.Revision {
	return model.Revision{Generation: 1, Replica: replicaA}
}

func validProject() model.Project {
	return model.Project{
		Key:             model.GlobalProjectKey,
		Slug:            model.GlobalProjectSlug,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		CreationReplica: replicaA,
		Revision:        validRevision(),
	}
}

func validIssue() model.Issue {
	return model.Issue{
		ID:              "br-6vf",
		ProjectKey:      model.GlobalProjectKey,
		Title:           "Opaque IDs survive",
		Type:            model.TypeTask,
		Status:          model.StatusOpen,
		Priority:        2,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		Tombstone:       model.Live,
		CreationReplica: replicaA,
		Revision:        validRevision(),
	}
}

func validMemory() model.Memory {
	return model.Memory{
		ID:              "mem-old-shape",
		ProjectKey:      model.GlobalProjectKey,
		Title:           "A durable note",
		Body:            "The body remains searchable.",
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		Tombstone:       model.Live,
		CreationReplica: replicaA,
		Revision:        validRevision(),
	}
}

func validComment() model.Comment {
	return model.Comment{
		ID:              "c1",
		IssueID:         "br-6vf",
		Author:          "opencode",
		Body:            "a note",
		CreatedAt:       stamp,
		CreationReplica: replicaA,
	}
}

func validDependency() model.Dependency {
	return model.Dependency{
		FromID:    "br-6vf",
		ToID:      "k3f9x",
		Type:      model.DepBlocks,
		CreatedAt: stamp,
		Tombstone: model.Live,
		Revision:  validRevision(),
	}
}

func validLabel() model.Label {
	return model.Label{
		IssueID:   "br-6vf",
		Name:      "wayfinder:task",
		Tombstone: model.Live,
		Revision:  validRevision(),
	}
}

func validIssueParent() model.IssueParent {
	return model.IssueParent{
		ChildID:   "dw32p.17",
		ParentID:  "dw32p",
		CreatedAt: stamp,
		Tombstone: model.Live,
		Revision:  validRevision(),
	}
}

func validLocator() model.RepositoryLocator {
	return model.RepositoryLocator{
		ProjectKey: model.GlobalProjectKey,
		Locator:    "git@example.com:org/repo.git",
		Tombstone:  model.Live,
		Revision:   validRevision(),
	}
}

func validSuccession() model.ReplicaSuccession {
	return model.ReplicaSuccession{
		OldReplica: replicaA,
		NewReplica: replicaB,
		RekeyedAt:  stamp,
	}
}
