package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/tedkulp/drops/internal/model"
)

// Like an Issue, a Memory's creation origin lives in its ID reservation.
const memoryInsertColumns = `id, project_key, title, body, provenance, created_at, updated_at,
                             superseded_by, tombstoned, revision_generation, revision_replica`

const memorySelect = `SELECT memories.id, memories.project_key, memories.title, memories.body,
                             memories.provenance, memories.created_at, memories.updated_at,
                             memories.superseded_by, memories.tombstoned, owner.creation_replica,
                             memories.revision_generation, memories.revision_replica
                      FROM memories
                      JOIN id_owners AS owner ON owner.id = memories.id AND owner.kind = 'memory'`

func scanMemory(row scanner) (model.Memory, error) {
	var (
		memory       model.Memory
		provenance   sql.NullString
		supersededBy sql.NullString
		tombstoned   int
	)
	err := row.Scan(
		&memory.ID,
		&memory.ProjectKey,
		&memory.Title,
		&memory.Body,
		&provenance,
		&memory.CreatedAt,
		&memory.UpdatedAt,
		&supersededBy,
		&tombstoned,
		&memory.CreationReplica,
		&memory.Revision.Generation,
		&memory.Revision.Replica,
	)
	if err != nil {
		return model.Memory{}, err
	}
	memory.Provenance = textPtr(provenance)
	memory.SupersededBy = idPtr(supersededBy)
	memory.Tombstone = tombstoneFrom(tombstoned)
	return memory, nil
}

// Memory reads one Memory by ID, superseded or tombstoned included: a superseded
// Memory stays directly addressable, which is the whole point of preserving it.
func (read reader) Memory(ctx context.Context, id model.ID) (model.Memory, error) {
	row := read.ex.QueryRowContext(ctx, memorySelect+` WHERE memories.id = ?`, string(id))
	memory, err := scanMemory(row)
	if err != nil {
		return model.Memory{}, wrapNotFound(fmt.Sprintf("Memory %s", id), err)
	}
	return memory, nil
}

// MemoryFilter selects Memories. A zero filter selects every current Memory in
// every Project: live, and not replaced by a later one.
type MemoryFilter struct {
	// Project scopes to one Project. Nil spans the whole store, which is what
	// a cross-project search does.
	Project *model.ProjectKey
	// IncludeSuperseded adds Memories that a later Memory has replaced.
	IncludeSuperseded bool
	// IncludeTombstoned adds removed Memories.
	IncludeTombstoned bool
	// Limit caps the result; zero or less means every match.
	Limit int
}

func (filter MemoryFilter) predicates() ([]string, []any) {
	var (
		predicates []string
		args       []any
	)
	if !filter.IncludeTombstoned {
		predicates = append(predicates, "memories.tombstoned = 0")
	}
	if !filter.IncludeSuperseded {
		predicates = append(predicates, "memories.superseded_by IS NULL")
	}
	if filter.Project != nil {
		predicates = append(predicates, "memories.project_key = ?")
		args = append(args, string(*filter.Project))
	}
	return predicates, args
}

// Memories lists matching Memories newest first, then by ID.
func (read reader) Memories(ctx context.Context, filter MemoryFilter) ([]model.Memory, error) {
	predicates, args := filter.predicates()
	query := memorySelect
	if len(predicates) > 0 {
		query += " WHERE " + strings.Join(predicates, " AND ")
	}
	query += " ORDER BY memories.updated_at DESC, memories.id"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := read.ex.QueryContext(ctx, query, args...)
	return collect(rows, err, "list Memories", scanMemory)
}

// PutMemory inserts a Memory whose ID is already reserved to it, on the same
// terms as PutIssue.
func (tx *Tx) PutMemory(ctx context.Context, memory model.Memory) error {
	if err := memory.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO memories (` + memoryInsertColumns + `)
	                   SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
	                   WHERE EXISTS (SELECT 1 FROM id_owners
	                                 WHERE id = ? AND kind = 'memory' AND creation_replica = ?)`
	result, err := tx.ex.ExecContext(ctx, statement,
		string(memory.ID),
		string(memory.ProjectKey),
		memory.Title,
		memory.Body,
		nullText(memory.Provenance),
		string(memory.CreatedAt),
		string(memory.UpdatedAt),
		nullID(memory.SupersededBy),
		tombstoneValue(memory.Tombstone),
		memory.Revision.Generation,
		string(memory.Revision.Replica),
		string(memory.ID),
		string(memory.CreationReplica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert Memory %s", memory.ID), err)
	}
	changed, err := affected(result, "insert Memory")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.explainReservation(ctx, memory.ID, model.OwnerMemory, memory.CreationReplica)
	}
	return tx.bumpWriteSeq(ctx)
}

// UpdateMemory replaces a Memory whose stored revision is still observed. A
// supersession link names a Memory in the same Project; the schema enforces
// that, so pointing at one elsewhere is ErrNotFound.
func (tx *Tx) UpdateMemory(ctx context.Context, memory model.Memory, observed model.Revision) error {
	if err := memory.Validate(); err != nil {
		return err
	}
	const statement = `UPDATE memories
	                   SET project_key = ?, title = ?, body = ?, provenance = ?,
	                       created_at = ?, updated_at = ?, superseded_by = ?, tombstoned = ?,
	                       revision_generation = ?, revision_replica = ?
	                   WHERE id = ?
	                     AND revision_generation = ? AND revision_replica = ?`
	result, err := tx.ex.ExecContext(ctx, statement,
		string(memory.ProjectKey),
		memory.Title,
		memory.Body,
		nullText(memory.Provenance),
		string(memory.CreatedAt),
		string(memory.UpdatedAt),
		nullID(memory.SupersededBy),
		tombstoneValue(memory.Tombstone),
		memory.Revision.Generation,
		string(memory.Revision.Replica),
		string(memory.ID),
		observed.Generation,
		string(observed.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("update Memory %s", memory.ID), err)
	}
	changed, err := affected(result, "update Memory")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.resolveMiss(ctx, fmt.Sprintf("Memory %s", memory.ID),
			`SELECT count(*) FROM memories WHERE id = ?`, string(memory.ID))
	}
	return tx.bumpWriteSeq(ctx)
}
