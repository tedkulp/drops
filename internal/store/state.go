package store

import (
	"context"
	"fmt"
)

// State is the store's local bookkeeping: how many replicated writes it has
// taken, how many of those the mirror has exported, and the head that export
// produced.
type State struct {
	// WriteSeq counts replicated writes and only ever increases.
	WriteSeq int64
	// ExportedWriteSeq is the WriteSeq the last export captured. Equal values
	// mean the current replicated state has been exported.
	ExportedWriteSeq int64
	// LastExportHead is empty before this store's first export.
	LastExportHead string
}

// Exported reports whether every replicated write has reached the mirror.
func (state State) Exported() bool { return state.WriteSeq == state.ExportedWriteSeq }

func (read reader) State(ctx context.Context) (State, error) {
	const query = `SELECT write_seq, exported_write_seq, coalesce(last_export_head, '')
	               FROM store_state WHERE id = 1`
	var state State
	err := read.ex.QueryRowContext(ctx, query).
		Scan(&state.WriteSeq, &state.ExportedWriteSeq, &state.LastExportHead)
	if err != nil {
		return State{}, wrapNotFound("store state", err)
	}
	return state, nil
}

// bumpWriteSeq marks the store changed. Every replicated write calls it, in the
// same transaction as the write itself, so a committed change can never be
// invisible to the mirror. Local bookkeeping — FTS maintenance, export marks,
// workspace bindings, replica heads — deliberately does not.
func (tx *Tx) bumpWriteSeq(ctx context.Context) error {
	const statement = `UPDATE store_state SET write_seq = write_seq + 1 WHERE id = 1`
	result, err := tx.ex.ExecContext(ctx, statement)
	if err != nil {
		return classify("bump write sequence", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("bump write sequence: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("bump write sequence: store_state singleton is missing")
	}
	return nil
}

// MarkExported records that an export captured the store at seq, producing head.
// It is not itself a replicated write.
func (tx *Tx) MarkExported(ctx context.Context, seq int64, head string) error {
	const statement = `UPDATE store_state
	                   SET exported_write_seq = ?, last_export_head = ?
	                   WHERE id = 1`
	if _, err := tx.ex.ExecContext(ctx, statement, seq, head); err != nil {
		return classify("mark exported", err)
	}
	return nil
}
