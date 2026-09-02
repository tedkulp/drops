package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// Every test runs against a real SQLite file. There is one adapter and no Store
// interface, so a fake would only ever prove itself right.

func newStore(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "drops.db")
	opened, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { opened.Close() })
	return opened
}

// write runs one transaction and fails the test if it does not commit.
func write(t *testing.T, opened *store.Store, fn func(context.Context, *store.Tx) error) {
	t.Helper()
	if err := opened.WithTx(t.Context(), fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

// writeErr runs one transaction and returns its error for inspection.
func writeErr(t *testing.T, opened *store.Store, fn func(context.Context, *store.Tx) error) error {
	t.Helper()
	return opened.WithTx(t.Context(), fn)
}

func newReplicaKey(t *testing.T) model.ReplicaKey {
	t.Helper()
	key, err := model.NewReplicaKey()
	if err != nil {
		t.Fatalf("mint replica key: %v", err)
	}
	return key
}

func newProjectKey(t *testing.T) model.ProjectKey {
	t.Helper()
	key, err := model.NewProjectKey()
	if err != nil {
		t.Fatalf("mint project key: %v", err)
	}
	return key
}

const (
	stampCreated = model.Timestamp("2026-08-28T11:22:33.123456789Z")
	stampUpdated = model.Timestamp("2026-08-29T09:00:00Z")
)

func text(value string) *string { return &value }

func stamp(value model.Timestamp) *model.Timestamp { return &value }

// newProject builds a valid Project under replica, leaving optional fields unset.
func newProject(t *testing.T, replica model.ReplicaKey, slug string) model.Project {
	t.Helper()
	revision, err := model.InitialRevision(replica)
	if err != nil {
		t.Fatalf("initial revision: %v", err)
	}
	return model.Project{
		Key:             newProjectKey(t),
		Slug:            slug,
		CreatedAt:       stampCreated,
		UpdatedAt:       stampUpdated,
		CreationReplica: replica,
		Revision:        revision,
	}
}

// seedProject writes a Project and returns it, for tests whose subject is
// something the Project has to exist for.
func seedProject(t *testing.T, opened *store.Store, replica model.ReplicaKey, slug string) model.Project {
	t.Helper()
	project := newProject(t, replica, slug)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutProject(ctx, project)
	})
	return project
}

func stateOf(t *testing.T, opened *store.Store) store.State {
	t.Helper()
	state, err := opened.State(t.Context())
	if err != nil {
		t.Fatalf("read store state: %v", err)
	}
	return state
}

// newIssue builds a valid Issue in project, leaving optional fields unset.
func newIssue(t *testing.T, project model.Project, id model.ID, title string) model.Issue {
	t.Helper()
	revision, err := model.InitialRevision(project.CreationReplica)
	if err != nil {
		t.Fatalf("initial revision: %v", err)
	}
	return model.Issue{
		ID:              id,
		ProjectKey:      project.Key,
		Title:           title,
		Type:            model.TypeTask,
		Status:          model.StatusOpen,
		Priority:        2,
		CreatedAt:       stampCreated,
		UpdatedAt:       stampUpdated,
		CreationReplica: project.CreationReplica,
		Revision:        revision,
	}
}

// seedIssue reserves the ID and writes the Issue, the way core composes the two.
func seedIssue(t *testing.T, opened *store.Store, project model.Project, id model.ID, title string) model.Issue {
	t.Helper()
	issue := newIssue(t, project, id, title)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID:              id,
			Kind:            model.OwnerIssue,
			CreationReplica: project.CreationReplica,
		}); err != nil {
			return err
		}
		return tx.PutIssue(ctx, issue)
	})
	return issue
}

func newMemory(t *testing.T, project model.Project, id model.ID, title, body string) model.Memory {
	t.Helper()
	revision, err := model.InitialRevision(project.CreationReplica)
	if err != nil {
		t.Fatalf("initial revision: %v", err)
	}
	return model.Memory{
		ID:              id,
		ProjectKey:      project.Key,
		Title:           title,
		Body:            body,
		CreatedAt:       stampCreated,
		UpdatedAt:       stampUpdated,
		CreationReplica: project.CreationReplica,
		Revision:        revision,
	}
}

func seedMemory(t *testing.T, opened *store.Store, project model.Project, id model.ID, title, body string) model.Memory {
	t.Helper()
	memory := newMemory(t, project, id, title, body)
	write(t, opened, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID:              id,
			Kind:            model.OwnerMemory,
			CreationReplica: project.CreationReplica,
		}); err != nil {
			return err
		}
		return tx.PutMemory(ctx, memory)
	})
	return memory
}

// ids returns the IDs of a listing, for order-sensitive assertions.
func issueIDs(issues []model.Issue) []model.ID {
	found := make([]model.ID, 0, len(issues))
	for _, issue := range issues {
		found = append(found, issue.ID)
	}
	return found
}
