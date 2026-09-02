package core

import (
	"context"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// CreateProject creates one synced Project. Slugs are unique but mutable;
// identity is the generated opaque Project key.
func (core *Core) CreateProject(ctx context.Context, slug string) (model.Project, error) {
	if slug == "" || isReservedSlug(slug) {
		return model.Project{}, fmt.Errorf("%w: Project slug %q is reserved or empty", model.ErrInvalid, slug)
	}
	key, err := model.NewProjectKey()
	if err != nil {
		return model.Project{}, fmt.Errorf("generate Project key: %w", err)
	}
	revision, err := model.InitialRevision(core.replica)
	if err != nil {
		return model.Project{}, err
	}
	stamp := core.timestamp()
	project := model.Project{
		Key:             key,
		Slug:            slug,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		CreationReplica: core.replica,
		Revision:        revision,
	}
	err = core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		return tx.PutProject(ctx, project)
	})
	if err != nil {
		return model.Project{}, err
	}
	return project, nil
}

// RenameProject changes the human-facing slug without changing identity.
func (core *Core) RenameProject(ctx context.Context, key model.ProjectKey, slug string) (model.Project, error) {
	if isReservedProject(key) {
		return model.Project{}, fmt.Errorf("%w: reserved Projects cannot be renamed", model.ErrInvalid)
	}
	if slug == "" || isReservedSlug(slug) {
		return model.Project{}, fmt.Errorf("%w: Project slug %q is reserved or empty", model.ErrInvalid, slug)
	}
	var changed model.Project
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := tx.Project(ctx, key)
		if err != nil {
			return err
		}
		next, err := current.Revision.Next(core.replica)
		if err != nil {
			return err
		}
		changed = current
		changed.Slug = slug
		changed.UpdatedAt = core.timestamp()
		changed.Revision = next
		return tx.UpdateProject(ctx, changed, current.Revision)
	})
	if err != nil {
		return model.Project{}, err
	}
	return changed, nil
}

// ArchiveProject retires a Project while preserving its identity and records.
func (core *Core) ArchiveProject(ctx context.Context, key model.ProjectKey) (model.Project, error) {
	if isReservedProject(key) {
		return model.Project{}, fmt.Errorf("%w: reserved Projects cannot be archived", model.ErrInvalid)
	}
	var changed model.Project
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := tx.Project(ctx, key)
		if err != nil {
			return err
		}
		if current.ArchivedAt != nil {
			return fmt.Errorf("%w: Project %s is already archived", model.ErrConflict, key)
		}
		next, err := current.Revision.Next(core.replica)
		if err != nil {
			return err
		}
		stamp := core.timestamp()
		changed = current
		changed.ArchivedAt = &stamp
		changed.UpdatedAt = stamp
		changed.Revision = next
		return tx.UpdateProject(ctx, changed, current.Revision)
	})
	if err != nil {
		return model.Project{}, err
	}
	return changed, nil
}
