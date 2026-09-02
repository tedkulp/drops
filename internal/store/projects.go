package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

const projectColumns = `project_key, slug, archived_at, created_at, updated_at,
                        creation_replica, revision_generation, revision_replica`

func scanProject(row scanner) (model.Project, error) {
	var (
		project    model.Project
		archivedAt sql.NullString
	)
	err := row.Scan(
		&project.Key,
		&project.Slug,
		&archivedAt,
		&project.CreatedAt,
		&project.UpdatedAt,
		&project.CreationReplica,
		&project.Revision.Generation,
		&project.Revision.Replica,
	)
	if err != nil {
		return model.Project{}, err
	}
	project.ArchivedAt = stampPtr(archivedAt)
	return project, nil
}

// Project reads one Project by its immutable key.
func (read reader) Project(ctx context.Context, key model.ProjectKey) (model.Project, error) {
	row := read.ex.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE project_key = ?`, string(key))
	project, err := scanProject(row)
	if err != nil {
		return model.Project{}, wrapNotFound(fmt.Sprintf("Project %s", key), err)
	}
	return project, nil
}

// ProjectBySlug reads one Project by its human-facing name. A slug is unique
// within a store but is not identity: it can be reassigned by a rename.
func (read reader) ProjectBySlug(ctx context.Context, slug string) (model.Project, error) {
	row := read.ex.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE slug = ?`, slug)
	project, err := scanProject(row)
	if err != nil {
		return model.Project{}, wrapNotFound(fmt.Sprintf("Project %q", slug), err)
	}
	return project, nil
}

// Projects lists every Project in key order. Projects are never tombstoned;
// archival is a field, so an archived Project is still listed here.
func (read reader) Projects(ctx context.Context) ([]model.Project, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+projectColumns+` FROM projects ORDER BY project_key`)
	return collect(rows, err, "list Projects", scanProject)
}

// PutProject inserts a Project that this store has not seen. A key that is
// already taken is ErrConflict; changing an existing Project goes through
// UpdateProject so its revision is checked.
func (tx *Tx) PutProject(ctx context.Context, project model.Project) error {
	if err := project.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO projects (` + projectColumns + `)
	                   VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(project.Key),
		project.Slug,
		nullStamp(project.ArchivedAt),
		string(project.CreatedAt),
		string(project.UpdatedAt),
		string(project.CreationReplica),
		project.Revision.Generation,
		string(project.Revision.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert Project %s", project.Key), err)
	}
	return tx.bumpWriteSeq(ctx)
}

// UpdateProject replaces a Project whose stored revision is still observed.
// Creation origin is immutable and is never written by an update.
func (tx *Tx) UpdateProject(ctx context.Context, project model.Project, observed model.Revision) error {
	if err := project.Validate(); err != nil {
		return err
	}
	const statement = `UPDATE projects
	                   SET slug = ?, archived_at = ?, created_at = ?, updated_at = ?,
	                       revision_generation = ?, revision_replica = ?
	                   WHERE project_key = ?
	                     AND revision_generation = ? AND revision_replica = ?`
	result, err := tx.ex.ExecContext(ctx, statement,
		project.Slug,
		nullStamp(project.ArchivedAt),
		string(project.CreatedAt),
		string(project.UpdatedAt),
		project.Revision.Generation,
		string(project.Revision.Replica),
		string(project.Key),
		observed.Generation,
		string(observed.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("update Project %s", project.Key), err)
	}
	changed, err := affected(result, "update Project")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.resolveMiss(ctx, fmt.Sprintf("Project %s", project.Key),
			`SELECT count(*) FROM projects WHERE project_key = ?`, string(project.Key))
	}
	return tx.bumpWriteSeq(ctx)
}
