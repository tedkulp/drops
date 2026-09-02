package store

import (
	"context"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

// Workspace bindings, accepted replica heads and replica succession are local
// records. None of them is replicated, so none of them bumps the write counter:
// a machine's own paths and transport bookkeeping must never make the store look
// dirty to the mirror.

// WorkspaceBinding reads the Project bound to one canonical absolute path.
func (read reader) WorkspaceBinding(ctx context.Context, path string) (model.WorkspaceBinding, error) {
	var binding model.WorkspaceBinding
	err := read.ex.QueryRowContext(ctx,
		`SELECT path, project_key FROM workspace_bindings WHERE path = ?`, path).
		Scan(&binding.Path, &binding.ProjectKey)
	if err != nil {
		return model.WorkspaceBinding{}, wrapNotFound(fmt.Sprintf("workspace binding %q", path), err)
	}
	return binding, nil
}

// WorkspaceBindings lists every binding in path order.
func (read reader) WorkspaceBindings(ctx context.Context) ([]model.WorkspaceBinding, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT path, project_key FROM workspace_bindings ORDER BY path`)
	return collect(rows, err, "list workspace bindings", func(row scanner) (model.WorkspaceBinding, error) {
		var binding model.WorkspaceBinding
		err := row.Scan(&binding.Path, &binding.ProjectKey)
		return binding, err
	})
}

// PutWorkspaceBinding binds a path to a Project, replacing any binding that path
// already had. A path names at most one Project, so rebinding is an overwrite
// rather than a conflict.
func (tx *Tx) PutWorkspaceBinding(ctx context.Context, binding model.WorkspaceBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO workspace_bindings (path, project_key) VALUES (?, ?)
	                   ON CONFLICT (path) DO UPDATE SET project_key = excluded.project_key`
	_, err := tx.ex.ExecContext(ctx, statement, binding.Path, string(binding.ProjectKey))
	if err != nil {
		return classify(fmt.Sprintf("bind workspace %q", binding.Path), err)
	}
	return nil
}

// DeleteWorkspaceBinding unbinds a path.
func (tx *Tx) DeleteWorkspaceBinding(ctx context.Context, path string) error {
	result, err := tx.ex.ExecContext(ctx, `DELETE FROM workspace_bindings WHERE path = ?`, path)
	if err != nil {
		return classify(fmt.Sprintf("unbind workspace %q", path), err)
	}
	changed, err := affected(result, "unbind workspace")
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("%w: workspace binding %q", model.ErrNotFound, path)
	}
	return nil
}

// ReplicaHead reads the snapshot head last accepted from one Replica.
func (read reader) ReplicaHead(ctx context.Context, replica model.ReplicaKey) (model.ReplicaHead, error) {
	var head model.ReplicaHead
	err := read.ex.QueryRowContext(ctx,
		`SELECT replica_key, snapshot_head FROM replica_heads WHERE replica_key = ?`,
		string(replica)).Scan(&head.Replica, &head.SnapshotHead)
	if err != nil {
		return model.ReplicaHead{}, wrapNotFound(fmt.Sprintf("head of Replica %s", replica), err)
	}
	return head, nil
}

// ReplicaHeads lists every accepted head in Replica-key order. An import proves
// itself against these: a second, different successor to a head it already holds
// is a fork.
func (read reader) ReplicaHeads(ctx context.Context) ([]model.ReplicaHead, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT replica_key, snapshot_head FROM replica_heads ORDER BY replica_key`)
	return collect(rows, err, "list replica heads", func(row scanner) (model.ReplicaHead, error) {
		var head model.ReplicaHead
		err := row.Scan(&head.Replica, &head.SnapshotHead)
		return head, err
	})
}

// PutReplicaHead records the head accepted from one Replica, advancing whatever
// was there before.
func (tx *Tx) PutReplicaHead(ctx context.Context, head model.ReplicaHead) error {
	if err := head.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO replica_heads (replica_key, snapshot_head) VALUES (?, ?)
	                   ON CONFLICT (replica_key) DO UPDATE SET snapshot_head = excluded.snapshot_head`
	_, err := tx.ex.ExecContext(ctx, statement, string(head.Replica), head.SnapshotHead)
	if err != nil {
		return classify(fmt.Sprintf("record head of Replica %s", head.Replica), err)
	}
	return nil
}

// ReplicaSuccessions lists recorded rekeys, oldest key first by key order. They
// travel in a snapshot as deterministic metadata and are never merged as domain
// records.
func (read reader) ReplicaSuccessions(ctx context.Context) ([]model.ReplicaSuccession, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT old_replica_key, new_replica_key, rekeyed_at
		 FROM replica_successions ORDER BY old_replica_key`)
	return collect(rows, err, "list replica successions", func(row scanner) (model.ReplicaSuccession, error) {
		var succession model.ReplicaSuccession
		err := row.Scan(&succession.OldReplica, &succession.NewReplica, &succession.RekeyedAt)
		return succession, err
	})
}

// PutReplicaSuccession records one rekey. Succession is one-to-one in both
// directions — each key is superseded once and supersedes once — so reusing
// either key is ErrConflict.
func (tx *Tx) PutReplicaSuccession(ctx context.Context, succession model.ReplicaSuccession) error {
	if err := succession.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO replica_successions (old_replica_key, new_replica_key, rekeyed_at)
	                   VALUES (?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(succession.OldReplica), string(succession.NewReplica), string(succession.RekeyedAt))
	if err != nil {
		return classify(fmt.Sprintf("record succession of Replica %s", succession.OldReplica), err)
	}
	return nil
}
