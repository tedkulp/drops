package core

import (
	"context"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// RecordReplicaSuccession records one irreversible lineage replacement after
// validating the resulting one-to-one acyclic chain.
func (core *Core) RecordReplicaSuccession(ctx context.Context, oldKey, newKey model.ReplicaKey) (model.ReplicaSuccession, error) {
	succession := model.ReplicaSuccession{OldReplica: oldKey, NewReplica: newKey, RekeyedAt: core.timestamp()}
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutReplicaSuccession(ctx, succession); err != nil {
			return err
		}
		return validateImportedGraphs(ctx, tx)
	})
	return succession, err
}

func (core *Core) ReplicaHeads(ctx context.Context) ([]model.ReplicaHead, error) {
	return core.store.ReplicaHeads(ctx)
}

func (core *Core) ReplicaSuccessions(ctx context.Context) ([]model.ReplicaSuccession, error) {
	return core.store.ReplicaSuccessions(ctx)
}
