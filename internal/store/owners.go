package store

import (
	"context"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

// IDOwner reads the permanent reservation of one opaque ID.
func (read reader) IDOwner(ctx context.Context, id model.ID) (model.IDOwner, error) {
	const query = `SELECT id, kind, creation_replica FROM id_owners WHERE id = ?`
	var owner model.IDOwner
	err := read.ex.QueryRowContext(ctx, query, string(id)).
		Scan(&owner.ID, &owner.Kind, &owner.CreationReplica)
	if err != nil {
		return model.IDOwner{}, wrapNotFound(fmt.Sprintf("ID owner %s", id), err)
	}
	return owner, nil
}

// PutIDOwner reserves an ID to one kind, permanently. A taken ID is ErrConflict
// whichever kind claims it: Issues and Memories share one namespace, and the
// composite foreign key on each table makes that structural rather than a sweep
// doctor has to run.
//
// The reservation is not itself replicated state — creation origin travels
// inline on the Issue or Memory record — so it does not bump the write counter.
// Core writes it in the same transaction as the entity it reserves for.
func (tx *Tx) PutIDOwner(ctx context.Context, owner model.IDOwner) error {
	if err := owner.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO id_owners (id, kind, creation_replica) VALUES (?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(owner.ID), string(owner.Kind), string(owner.CreationReplica))
	if err != nil {
		return classify(fmt.Sprintf("reserve ID %s", owner.ID), err)
	}
	return nil
}

// IDOwners lists every reservation in ID order.
func (read reader) IDOwners(ctx context.Context) ([]model.IDOwner, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT id, kind, creation_replica FROM id_owners ORDER BY id`)
	return collect(rows, err, "list ID owners", func(row scanner) (model.IDOwner, error) {
		var owner model.IDOwner
		err := row.Scan(&owner.ID, &owner.Kind, &owner.CreationReplica)
		return owner, err
	})
}
