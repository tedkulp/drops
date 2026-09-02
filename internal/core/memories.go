package core

import (
	"context"
	"errors"
	"fmt"

	memorytext "github.com/tedkulp/drops/internal/memory"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// CreateMemory is caller-authored notebook content. Core derives a title when
// omitted and supplies identity, revision, timestamps, and ownership.
type CreateMemory struct {
	Project    model.ProjectKey
	Title      string
	Body       string
	Provenance string
}

// CreateMemory atomically reserves an ID and creates one Memory.
func (core *Core) CreateMemory(ctx context.Context, input CreateMemory) (model.Memory, error) {
	if input.Body == "" {
		return model.Memory{}, fmt.Errorf("%w: Memory body is empty", model.ErrInvalid)
	}
	if input.Title == "" {
		input.Title = memorytext.DeriveTitle(input.Body)
	}
	project, err := core.store.Project(ctx, input.Project)
	if err != nil {
		return model.Memory{}, err
	}
	if project.ArchivedAt != nil {
		return model.Memory{}, fmt.Errorf("%w: Project %s is archived", model.ErrInvalid, input.Project)
	}

	for range 100 {
		id, err := core.nextID(ctx, model.OwnerMemory, nil)
		if err != nil {
			return model.Memory{}, err
		}
		created, err := core.putNewMemory(ctx, id, input)
		if errors.Is(err, model.ErrConflict) {
			continue
		}
		if err == nil {
			core.emitWarnings(model.RecordMemory, string(created.ID), []textField{{"title", created.Title}, {"body", created.Body}})
		}
		return created, err
	}
	return model.Memory{}, fmt.Errorf("%w: could not reserve a Memory ID", model.ErrConflict)
}

func (core *Core) nextID(ctx context.Context, kind model.OwnerKind, parent *model.ID) (model.ID, error) {
	owners, err := core.store.IDOwners(ctx)
	if err != nil {
		return "", err
	}
	reserved := make([]model.ID, len(owners))
	for i := range owners {
		reserved[i] = owners[i].ID
	}
	core.idMu.Lock()
	defer core.idMu.Unlock()
	return core.ids.NewID(kind, parent, reserved)
}

func (core *Core) putNewMemory(ctx context.Context, id model.ID, input CreateMemory) (model.Memory, error) {
	revision, err := model.InitialRevision(core.replica)
	if err != nil {
		return model.Memory{}, err
	}
	stamp := core.timestamp()
	entry := model.Memory{
		ID:              id,
		ProjectKey:      input.Project,
		Title:           input.Title,
		Body:            input.Body,
		Provenance:      memorytext.NormalizeProvenance(input.Provenance),
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		CreationReplica: core.replica,
		Revision:        revision,
	}
	err = core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{ID: id, Kind: model.OwnerMemory, CreationReplica: core.replica}); err != nil {
			return err
		}
		return tx.PutMemory(ctx, entry)
	})
	if err != nil {
		return model.Memory{}, err
	}
	return entry, nil
}

// SupersedeMemory creates a replacement and links the old Memory in the same
// transaction. The replacement always inherits the old Project.
func (core *Core) SupersedeMemory(ctx context.Context, oldID model.ID, input CreateMemory) (model.Memory, error) {
	if input.Body == "" {
		return model.Memory{}, fmt.Errorf("%w: Memory body is empty", model.ErrInvalid)
	}
	if input.Title == "" {
		input.Title = memorytext.DeriveTitle(input.Body)
	}

	for range 100 {
		id, err := core.nextID(ctx, model.OwnerMemory, nil)
		if err != nil {
			return model.Memory{}, err
		}
		var replacement model.Memory
		err = core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
			old, err := tx.Memory(ctx, oldID)
			if err != nil {
				return err
			}
			if old.Tombstone == model.Tombstoned || old.SupersededBy != nil {
				return fmt.Errorf("%w: Memory %s is retired", model.ErrConflict, oldID)
			}
			input.Project = old.ProjectKey
			revision, err := model.InitialRevision(core.replica)
			if err != nil {
				return err
			}
			stamp := core.timestamp()
			replacement = model.Memory{
				ID:              id,
				ProjectKey:      old.ProjectKey,
				Title:           input.Title,
				Body:            input.Body,
				Provenance:      memorytext.NormalizeProvenance(input.Provenance),
				CreatedAt:       stamp,
				UpdatedAt:       stamp,
				CreationReplica: core.replica,
				Revision:        revision,
			}
			if err := tx.PutIDOwner(ctx, model.IDOwner{ID: id, Kind: model.OwnerMemory, CreationReplica: core.replica}); err != nil {
				return err
			}
			if err := tx.PutMemory(ctx, replacement); err != nil {
				return err
			}
			observed := old.Revision
			next, err := observed.Next(core.replica)
			if err != nil {
				return err
			}
			old.SupersededBy = &replacement.ID
			old.UpdatedAt = core.timestamp()
			old.Revision = next
			return tx.UpdateMemory(ctx, old, observed)
		})
		if errors.Is(err, model.ErrConflict) {
			if _, ownerErr := core.store.IDOwner(ctx, id); ownerErr == nil {
				continue
			}
		}
		if err == nil {
			core.emitWarnings(model.RecordMemory, string(replacement.ID), []textField{{"title", replacement.Title}, {"body", replacement.Body}})
		}
		return replacement, err
	}
	return model.Memory{}, fmt.Errorf("%w: could not reserve a Memory ID", model.ErrConflict)
}
