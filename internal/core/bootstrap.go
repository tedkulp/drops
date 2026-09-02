package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

const reservedProjectTimestamp model.Timestamp = "1970-01-01T00:00:00Z"

// Bootstrap ensures the two independently convergent reserved Projects exist.
func (core *Core) Bootstrap(ctx context.Context) error {
	return core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		for _, reserved := range []struct {
			key  model.ProjectKey
			slug string
		}{
			{model.GlobalProjectKey, "global"},
			{model.InboxProjectKey, "inbox"},
		} {
			existing, err := tx.Project(ctx, reserved.key)
			if err == nil {
				if existing.Slug != reserved.slug || existing.ArchivedAt != nil {
					return fmt.Errorf("%w: reserved Project %s is malformed", model.ErrConflict, reserved.key)
				}
				continue
			}
			if !isNotFound(err) {
				return err
			}
			origin := model.ReplicaKey(reserved.key)
			project := model.Project{
				Key: reserved.key, Slug: reserved.slug,
				CreatedAt: reservedProjectTimestamp, UpdatedAt: reservedProjectTimestamp,
				CreationReplica: origin, Revision: model.Revision{Generation: 1, Replica: origin},
			}
			if err := tx.PutProject(ctx, project); err != nil {
				return err
			}
		}
		return nil
	})
}

func isNotFound(err error) bool { return err != nil && errors.Is(err, model.ErrNotFound) }
