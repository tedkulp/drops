package sync

import (
	"context"
	"errors"
	"fmt"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/mirror"
)

// ErrExportHeadMismatch reports a store and its sidecar that disagree on the
// last export head: a sign of a torn restore, which must never author locally
// under a stale identity.
var ErrExportHeadMismatch = errors.New("database and sidecar disagree on the last export head")

// ensureSidecar returns the active sidecar, minting one the first time this
// store authors locally. A present but malformed sidecar is a hard error.
func (t *Transport) ensureSidecar() (*Sidecar, error) {
	sidecar, err := LoadSidecar(t.dir)
	if err != nil {
		return nil, err
	}
	if sidecar != nil {
		return sidecar, nil
	}
	return MintSidecar(t.dir)
}

// checkCoherence refuses local authorship when the sidecar and the store record
// different last export heads: a coherent pair is the proof this installation
// has not been restored under a mismatched identity.
func (t *Transport) checkCoherence(ctx context.Context, sidecar *Sidecar) error {
	state, err := t.store.State(ctx)
	if err != nil {
		return err
	}
	if sidecar.LastExportHead != state.LastExportHead {
		return fmt.Errorf("%w: sidecar %q, store %q",
			ErrExportHeadMismatch, sidecar.LastExportHead, state.LastExportHead)
	}
	return nil
}

// prepareExport captures one deterministic snapshot and its write sequence.
func (t *Transport) prepareExport(ctx context.Context, predecessors []string) (core.ExportBatch, []byte, error) {
	batch, err := t.core.PrepareExport(ctx, predecessors)
	if err != nil {
		return core.ExportBatch{}, nil, err
	}
	raw, err := mirror.Marshal(batch.Snapshot)
	if err != nil {
		return core.ExportBatch{}, nil, err
	}
	return batch, raw, nil
}

// writeMirror replaces the mirror file atomically.
func (t *Transport) writeMirror(raw []byte) error {
	return writeFileAtomic(t.dir, mirrorFileName, raw)
}

// recordExport stamps the export mark in the store and the matching head in the
// sidecar, keeping the pair coherent.
func (t *Transport) recordExport(ctx context.Context, writeSeq int64, head string) error {
	sidecar, err := t.ensureSidecar()
	if err != nil {
		return err
	}
	if err := t.core.MarkExported(ctx, writeSeq, head); err != nil {
		return err
	}
	sidecar.LastExportHead = head
	return sidecar.Save(t.dir)
}
