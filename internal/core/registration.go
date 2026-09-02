package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// Registration is trusted, already-normalized project discovery data from
// resolve or an explicit project command.
type Registration struct {
	Slug        string
	Locator     string
	BindingPath string
}

// RegisterProject creates a Project and its optional locator and local binding
// in one transaction. A lost same-slug insert converges on the winner.
func (core *Core) RegisterProject(ctx context.Context, input Registration) (model.Project, error) {
	if input.Slug == "" || isReservedSlug(input.Slug) {
		return model.Project{}, fmt.Errorf("%w: Project slug %q is reserved or empty", model.ErrInvalid, input.Slug)
	}
	if input.Locator == "" && input.BindingPath == "" {
		return core.CreateProject(ctx, input.Slug)
	}
	key, err := model.NewProjectKey()
	if err != nil {
		return model.Project{}, err
	}
	revision, err := model.InitialRevision(core.replica)
	if err != nil {
		return model.Project{}, err
	}
	stamp := core.timestamp()
	project := model.Project{Key: key, Slug: input.Slug, CreatedAt: stamp, UpdatedAt: stamp, CreationReplica: core.replica, Revision: revision}
	err = core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutProject(ctx, project); err != nil {
			return err
		}
		if input.Locator != "" {
			if err := tx.PutRepositoryLocator(ctx, model.RepositoryLocator{ProjectKey: key, Locator: input.Locator, Revision: revision}); err != nil {
				return err
			}
		}
		if input.BindingPath != "" {
			if err := tx.PutWorkspaceBinding(ctx, model.WorkspaceBinding{Path: input.BindingPath, ProjectKey: key}); err != nil {
				return err
			}
		}
		return nil
	})
	if !errors.Is(err, model.ErrConflict) {
		return project, err
	}

	winner, readErr := core.store.ProjectBySlug(ctx, input.Slug)
	if readErr != nil {
		return model.Project{}, err
	}
	err = core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if input.Locator != "" {
			matches, matchErr := tx.ProjectsByLocator(ctx, input.Locator)
			if matchErr != nil {
				return matchErr
			}
			alreadyPresent := false
			for _, match := range matches {
				alreadyPresent = alreadyPresent || match.ProjectKey == winner.Key
			}
			if !alreadyPresent {
				locatorRevision, revisionErr := model.InitialRevision(core.replica)
				if revisionErr != nil {
					return revisionErr
				}
				if putErr := tx.PutRepositoryLocator(ctx, model.RepositoryLocator{ProjectKey: winner.Key, Locator: input.Locator, Revision: locatorRevision}); putErr != nil {
					return putErr
				}
			}
		}
		if input.BindingPath != "" {
			return tx.PutWorkspaceBinding(ctx, model.WorkspaceBinding{Path: input.BindingPath, ProjectKey: winner.Key})
		}
		return nil
	})
	return winner, err
}

// SetRepositoryLocator adds, restores, or tombstones synced discovery evidence.
func (core *Core) SetRepositoryLocator(ctx context.Context, project model.ProjectKey, locator string, present bool) (model.RepositoryLocator, error) {
	if locator == "" {
		return model.RepositoryLocator{}, fmt.Errorf("%w: empty repository locator", model.ErrInvalid)
	}
	var changed model.RepositoryLocator
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, err := tx.Project(ctx, project); err != nil {
			return err
		}
		current, err := tx.RepositoryLocator(ctx, project, locator)
		if errors.Is(err, model.ErrNotFound) {
			if !present {
				return err
			}
			revision, revisionErr := model.InitialRevision(core.replica)
			if revisionErr != nil {
				return revisionErr
			}
			changed = model.RepositoryLocator{ProjectKey: project, Locator: locator, Revision: revision}
			return tx.PutRepositoryLocator(ctx, changed)
		}
		if err != nil {
			return err
		}
		desired := model.Tombstoned
		if present {
			desired = model.Live
		}
		if current.Tombstone == desired {
			changed = current
			return nil
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		current.Tombstone, current.Revision = desired, next
		changed = current
		return tx.UpdateRepositoryLocator(ctx, current, observed)
	})
	return changed, err
}

func (core *Core) BindWorkspace(ctx context.Context, binding model.WorkspaceBinding) error {
	return core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, err := tx.Project(ctx, binding.ProjectKey); err != nil {
			return err
		}
		return tx.PutWorkspaceBinding(ctx, binding)
	})
}

func (core *Core) UnbindWorkspace(ctx context.Context, path string) error {
	return core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		return tx.DeleteWorkspaceBinding(ctx, path)
	})
}
