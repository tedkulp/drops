package core

import (
	"context"

	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/store"
)

// ExportBatch is one coherent replicated snapshot and the write sequence it
// captured. Sync marks exactly this sequence exported after committing bytes.
type ExportBatch struct {
	Snapshot mirror.Snapshot `json:"snapshot"`
	WriteSeq int64           `json:"write_seq"`
}

// PrepareExport reads every replicated record and the write sequence in one
// transaction. Local records never cross the mirror seam.
func (core *Core) PrepareExport(ctx context.Context, predecessors []string) (ExportBatch, error) {
	var batch ExportBatch
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		projects, err := tx.Projects(ctx)
		if err != nil {
			return err
		}
		locators, err := tx.RepositoryLocators(ctx)
		if err != nil {
			return err
		}
		issues, err := tx.Issues(ctx, store.IssueFilter{IncludeTombstoned: true})
		if err != nil {
			return err
		}
		parents, err := tx.IssueParents(ctx)
		if err != nil {
			return err
		}
		dependencies, err := tx.Dependencies(ctx)
		if err != nil {
			return err
		}
		labels, err := tx.Labels(ctx)
		if err != nil {
			return err
		}
		comments, err := tx.Comments(ctx)
		if err != nil {
			return err
		}
		memories, err := tx.Memories(ctx, store.MemoryFilter{IncludeSuperseded: true, IncludeTombstoned: true})
		if err != nil {
			return err
		}
		successions, err := tx.ReplicaSuccessions(ctx)
		if err != nil {
			return err
		}
		state, err := tx.State(ctx)
		if err != nil {
			return err
		}

		records := make([]mirror.Record, 0, len(projects)+len(locators)+len(issues)+len(parents)+len(dependencies)+len(labels)+len(comments)+len(memories)+len(successions))
		for _, value := range projects {
			records = mustAppendRecord(records, value)
		}
		for _, value := range locators {
			records = mustAppendRecord(records, value)
		}
		for _, value := range issues {
			records = mustAppendRecord(records, value)
		}
		for _, value := range parents {
			records = mustAppendRecord(records, value)
		}
		for _, value := range dependencies {
			records = mustAppendRecord(records, value)
		}
		for _, value := range labels {
			records = mustAppendRecord(records, value)
		}
		for _, value := range comments {
			records = mustAppendRecord(records, value)
		}
		for _, value := range memories {
			records = mustAppendRecord(records, value)
		}
		for _, value := range successions {
			records = mustAppendRecord(records, value)
		}
		batch = ExportBatch{
			Snapshot: mirror.Snapshot{Predecessors: append([]string(nil), predecessors...), Records: records},
			WriteSeq: state.WriteSeq,
		}
		return nil
	})
	return batch, err
}

// Export returns only the snapshot for callers that do not own export state.
func (core *Core) Export(ctx context.Context, predecessors []string) (mirror.Snapshot, error) {
	batch, err := core.PrepareExport(ctx, predecessors)
	return batch.Snapshot, err
}

// MarkExported records the exact write sequence a committed mirror captured.
func (core *Core) MarkExported(ctx context.Context, writeSeq int64, head string) error {
	return core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		return tx.MarkExported(ctx, writeSeq, head)
	})
}

func mustAppendRecord(records []mirror.Record, value any) []mirror.Record {
	record, err := mirror.NewRecord(value)
	if err != nil {
		panic(err) // model values read from Store already passed the same validation
	}
	return append(records, record)
}
