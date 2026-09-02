package core

import (
	"context"
	"errors"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// IssueView is one coherent reading-page snapshot.
type IssueView struct {
	Issue        model.Issue         `json:"issue"`
	Project      model.Project       `json:"project"`
	Parent       *model.IssueParent  `json:"parent,omitempty"`
	Children     []model.IssueParent `json:"children"`
	Dependencies []model.Dependency  `json:"dependencies"`
	Dependents   []model.Dependency  `json:"dependents"`
	Labels       []model.Label       `json:"labels"`
	Comments     []model.Comment     `json:"comments"`
}

// ViewIssue reads an Issue and every child record rendered with it in one
// transaction, so a page cannot combine two revisions of the graph.
func (core *Core) ViewIssue(ctx context.Context, id model.ID) (IssueView, error) {
	var view IssueView
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		issue, err := tx.Issue(ctx, id)
		if err != nil {
			return err
		}
		project, err := tx.Project(ctx, issue.ProjectKey)
		if err != nil {
			return err
		}
		parent, err := tx.IssueParent(ctx, id)
		if err == nil && parent.Tombstone == model.Live {
			view.Parent = &parent
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		children, err := tx.IssueChildren(ctx, id)
		if err != nil {
			return err
		}
		dependencies, err := tx.DependenciesFrom(ctx, id)
		if err != nil {
			return err
		}
		dependents, err := tx.DependenciesTo(ctx, id)
		if err != nil {
			return err
		}
		labels, err := tx.IssueLabels(ctx, id)
		if err != nil {
			return err
		}
		comments, err := tx.IssueComments(ctx, id)
		if err != nil {
			return err
		}
		view.Issue, view.Project = issue, project
		view.Children, view.Dependencies, view.Dependents = children, dependencies, dependents
		view.Labels, view.Comments = labels, comments
		return nil
	})
	if view.Children == nil {
		view.Children = []model.IssueParent{}
	}
	if view.Dependencies == nil {
		view.Dependencies = []model.Dependency{}
	}
	if view.Dependents == nil {
		view.Dependents = []model.Dependency{}
	}
	if view.Labels == nil {
		view.Labels = []model.Label{}
	}
	if view.Comments == nil {
		view.Comments = []model.Comment{}
	}
	return view, err
}
