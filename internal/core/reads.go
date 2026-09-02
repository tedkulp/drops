package core

import (
	"context"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// Read filters and state are aliases because Core preserves Store's query
// semantics while keeping callers on the rules seam.
type IssueFilter = store.IssueFilter
type MemoryFilter = store.MemoryFilter
type State = store.State

func (core *Core) Project(ctx context.Context, key model.ProjectKey) (model.Project, error) {
	return core.store.Project(ctx, key)
}
func (core *Core) ProjectBySlug(ctx context.Context, slug string) (model.Project, error) {
	return core.store.ProjectBySlug(ctx, slug)
}
func (core *Core) Projects(ctx context.Context) ([]model.Project, error) {
	return core.store.Projects(ctx)
}
func (core *Core) ProjectsByLocator(ctx context.Context, locator string) ([]model.RepositoryLocator, error) {
	return core.store.ProjectsByLocator(ctx, locator)
}
func (core *Core) ProjectLocators(ctx context.Context, key model.ProjectKey) ([]model.RepositoryLocator, error) {
	return core.store.ProjectLocators(ctx, key)
}
func (core *Core) WorkspaceBindings(ctx context.Context) ([]model.WorkspaceBinding, error) {
	return core.store.WorkspaceBindings(ctx)
}
func (core *Core) Issue(ctx context.Context, id model.ID) (model.Issue, error) {
	return core.store.Issue(ctx, id)
}
func (core *Core) Issues(ctx context.Context, filter IssueFilter) ([]model.Issue, error) {
	return core.store.Issues(ctx, filter)
}
func (core *Core) SearchIssues(ctx context.Context, query string, filter IssueFilter) ([]model.Issue, error) {
	return core.store.SearchIssues(ctx, query, filter)
}
func (core *Core) IssueChildren(ctx context.Context, id model.ID) ([]model.IssueParent, error) {
	return core.store.IssueChildren(ctx, id)
}
func (core *Core) IssueComments(ctx context.Context, id model.ID) ([]model.Comment, error) {
	return core.store.IssueComments(ctx, id)
}
func (core *Core) IssueLabels(ctx context.Context, id model.ID) ([]model.Label, error) {
	return core.store.IssueLabels(ctx, id)
}
func (core *Core) DependenciesFrom(ctx context.Context, id model.ID) ([]model.Dependency, error) {
	return core.store.DependenciesFrom(ctx, id)
}
func (core *Core) DependenciesTo(ctx context.Context, id model.ID) ([]model.Dependency, error) {
	return core.store.DependenciesTo(ctx, id)
}
func (core *Core) Memory(ctx context.Context, id model.ID) (model.Memory, error) {
	return core.store.Memory(ctx, id)
}
func (core *Core) Memories(ctx context.Context, filter MemoryFilter) ([]model.Memory, error) {
	return core.store.Memories(ctx, filter)
}
func (core *Core) SearchMemories(ctx context.Context, query string, filter MemoryFilter) ([]model.Memory, error) {
	return core.store.SearchMemories(ctx, query, filter)
}
func (core *Core) State(ctx context.Context) (State, error) { return core.store.State(ctx) }
