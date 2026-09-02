package store

import (
	"context"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

// Parentage, dependencies and labels are each independently revisioned records,
// so a concurrent label change never conflicts with a concurrent status change.

const issueParentColumns = `child_id, parent_id, created_at, tombstoned,
                            revision_generation, revision_replica`

func scanIssueParent(row scanner) (model.IssueParent, error) {
	var (
		parent     model.IssueParent
		tombstoned int
	)
	err := row.Scan(
		&parent.ChildID,
		&parent.ParentID,
		&parent.CreatedAt,
		&tombstoned,
		&parent.Revision.Generation,
		&parent.Revision.Replica,
	)
	if err != nil {
		return model.IssueParent{}, err
	}
	parent.Tombstone = tombstoneFrom(tombstoned)
	return parent, nil
}

// IssueParent reads the single parent record of one child, live or tombstoned.
// The child's ID spelling is never consulted: this record is the only authority.
func (read reader) IssueParent(ctx context.Context, child model.ID) (model.IssueParent, error) {
	row := read.ex.QueryRowContext(ctx,
		`SELECT `+issueParentColumns+` FROM issue_parents WHERE child_id = ?`, string(child))
	parent, err := scanIssueParent(row)
	if err != nil {
		return model.IssueParent{}, wrapNotFound(fmt.Sprintf("parent of Issue %s", child), err)
	}
	return parent, nil
}

// IssueChildren lists the live parent records naming one parent, in child order.
func (read reader) IssueChildren(ctx context.Context, parent model.ID) ([]model.IssueParent, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+issueParentColumns+` FROM issue_parents
		 WHERE parent_id = ? AND tombstoned = 0 ORDER BY child_id`, string(parent))
	return collect(rows, err, "list Issue children", scanIssueParent)
}

// IssueParents lists every parent record, tombstoned included, for export.
func (read reader) IssueParents(ctx context.Context) ([]model.IssueParent, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+issueParentColumns+` FROM issue_parents ORDER BY child_id`)
	return collect(rows, err, "list Issue parents", scanIssueParent)
}

func (tx *Tx) PutIssueParent(ctx context.Context, parent model.IssueParent) error {
	if err := parent.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO issue_parents (` + issueParentColumns + `)
	                   VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(parent.ChildID),
		string(parent.ParentID),
		string(parent.CreatedAt),
		tombstoneValue(parent.Tombstone),
		parent.Revision.Generation,
		string(parent.Revision.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert parent of Issue %s", parent.ChildID), err)
	}
	return tx.bumpWriteSeq(ctx)
}

// UpdateIssueParent reparents or tombstones a child under compare-and-swap. A
// child has exactly one parent record, so a move is an update, not a second row.
func (tx *Tx) UpdateIssueParent(ctx context.Context, parent model.IssueParent, observed model.Revision) error {
	if err := parent.Validate(); err != nil {
		return err
	}
	const statement = `UPDATE issue_parents
	                   SET parent_id = ?, created_at = ?, tombstoned = ?,
	                       revision_generation = ?, revision_replica = ?
	                   WHERE child_id = ?
	                     AND revision_generation = ? AND revision_replica = ?`
	result, err := tx.ex.ExecContext(ctx, statement,
		string(parent.ParentID),
		string(parent.CreatedAt),
		tombstoneValue(parent.Tombstone),
		parent.Revision.Generation,
		string(parent.Revision.Replica),
		string(parent.ChildID),
		observed.Generation,
		string(observed.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("update parent of Issue %s", parent.ChildID), err)
	}
	changed, err := affected(result, "update Issue parent")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.resolveMiss(ctx, fmt.Sprintf("parent of Issue %s", parent.ChildID),
			`SELECT count(*) FROM issue_parents WHERE child_id = ?`, string(parent.ChildID))
	}
	return tx.bumpWriteSeq(ctx)
}

const dependencyColumns = `from_id, to_id, dep_type, created_at, tombstoned,
                           revision_generation, revision_replica`

func scanDependency(row scanner) (model.Dependency, error) {
	var (
		dependency model.Dependency
		tombstoned int
	)
	err := row.Scan(
		&dependency.FromID,
		&dependency.ToID,
		&dependency.Type,
		&dependency.CreatedAt,
		&tombstoned,
		&dependency.Revision.Generation,
		&dependency.Revision.Replica,
	)
	if err != nil {
		return model.Dependency{}, err
	}
	dependency.Tombstone = tombstoneFrom(tombstoned)
	return dependency, nil
}

// Dependency reads one relationship by its whole identity.
func (read reader) Dependency(ctx context.Context, from, to model.ID, kind model.DependencyType) (model.Dependency, error) {
	row := read.ex.QueryRowContext(ctx,
		`SELECT `+dependencyColumns+` FROM dependencies
		 WHERE from_id = ? AND to_id = ? AND dep_type = ?`,
		string(from), string(to), string(kind))
	dependency, err := scanDependency(row)
	if err != nil {
		return model.Dependency{}, wrapNotFound(
			fmt.Sprintf("dependency %s %s %s", from, kind, to), err)
	}
	return dependency, nil
}

// DependenciesFrom lists the live relationships an Issue declares.
func (read reader) DependenciesFrom(ctx context.Context, from model.ID) ([]model.Dependency, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+dependencyColumns+` FROM dependencies
		 WHERE from_id = ? AND tombstoned = 0 ORDER BY dep_type, to_id`, string(from))
	return collect(rows, err, "list dependencies from Issue", scanDependency)
}

// DependenciesTo lists the live relationships pointing at an Issue.
func (read reader) DependenciesTo(ctx context.Context, to model.ID) ([]model.Dependency, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+dependencyColumns+` FROM dependencies
		 WHERE to_id = ? AND tombstoned = 0 ORDER BY dep_type, from_id`, string(to))
	return collect(rows, err, "list dependencies to Issue", scanDependency)
}

// Dependencies lists every relationship, tombstoned included, for export.
func (read reader) Dependencies(ctx context.Context) ([]model.Dependency, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+dependencyColumns+` FROM dependencies ORDER BY from_id, to_id, dep_type`)
	return collect(rows, err, "list dependencies", scanDependency)
}

func (tx *Tx) PutDependency(ctx context.Context, dependency model.Dependency) error {
	if err := dependency.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO dependencies (` + dependencyColumns + `)
	                   VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(dependency.FromID),
		string(dependency.ToID),
		string(dependency.Type),
		string(dependency.CreatedAt),
		tombstoneValue(dependency.Tombstone),
		dependency.Revision.Generation,
		string(dependency.Revision.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert dependency %s %s %s",
			dependency.FromID, dependency.Type, dependency.ToID), err)
	}
	return tx.bumpWriteSeq(ctx)
}

// UpdateDependency changes a relationship's liveness under compare-and-swap. The
// three key columns are its identity and are never rewritten.
func (tx *Tx) UpdateDependency(ctx context.Context, dependency model.Dependency, observed model.Revision) error {
	if err := dependency.Validate(); err != nil {
		return err
	}
	const statement = `UPDATE dependencies
	                   SET created_at = ?, tombstoned = ?,
	                       revision_generation = ?, revision_replica = ?
	                   WHERE from_id = ? AND to_id = ? AND dep_type = ?
	                     AND revision_generation = ? AND revision_replica = ?`
	result, err := tx.ex.ExecContext(ctx, statement,
		string(dependency.CreatedAt),
		tombstoneValue(dependency.Tombstone),
		dependency.Revision.Generation,
		string(dependency.Revision.Replica),
		string(dependency.FromID),
		string(dependency.ToID),
		string(dependency.Type),
		observed.Generation,
		string(observed.Replica),
	)
	if err != nil {
		return classify("update dependency", err)
	}
	changed, err := affected(result, "update dependency")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.resolveMiss(ctx,
			fmt.Sprintf("dependency %s %s %s", dependency.FromID, dependency.Type, dependency.ToID),
			`SELECT count(*) FROM dependencies WHERE from_id = ? AND to_id = ? AND dep_type = ?`,
			string(dependency.FromID), string(dependency.ToID), string(dependency.Type))
	}
	return tx.bumpWriteSeq(ctx)
}

const labelColumns = `issue_id, label, tombstoned, revision_generation, revision_replica`

func scanLabel(row scanner) (model.Label, error) {
	var (
		label      model.Label
		tombstoned int
	)
	err := row.Scan(
		&label.IssueID,
		&label.Name,
		&tombstoned,
		&label.Revision.Generation,
		&label.Revision.Replica,
	)
	if err != nil {
		return model.Label{}, err
	}
	label.Tombstone = tombstoneFrom(tombstoned)
	return label, nil
}

// Label reads one membership, live or tombstoned.
func (read reader) Label(ctx context.Context, issue model.ID, name string) (model.Label, error) {
	row := read.ex.QueryRowContext(ctx,
		`SELECT `+labelColumns+` FROM labels WHERE issue_id = ? AND label = ?`,
		string(issue), name)
	label, err := scanLabel(row)
	if err != nil {
		return model.Label{}, wrapNotFound(fmt.Sprintf("label %q on Issue %s", name, issue), err)
	}
	return label, nil
}

// IssueLabels lists the live labels of one Issue, in name order.
func (read reader) IssueLabels(ctx context.Context, issue model.ID) ([]model.Label, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+labelColumns+` FROM labels
		 WHERE issue_id = ? AND tombstoned = 0 ORDER BY label`, string(issue))
	return collect(rows, err, "list Issue labels", scanLabel)
}

// Labels lists every membership, tombstoned included, for export.
func (read reader) Labels(ctx context.Context) ([]model.Label, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+labelColumns+` FROM labels ORDER BY issue_id, label`)
	return collect(rows, err, "list labels", scanLabel)
}

func (tx *Tx) PutLabel(ctx context.Context, label model.Label) error {
	if err := label.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO labels (` + labelColumns + `) VALUES (?, ?, ?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(label.IssueID),
		label.Name,
		tombstoneValue(label.Tombstone),
		label.Revision.Generation,
		string(label.Revision.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert label %q on Issue %s", label.Name, label.IssueID), err)
	}
	return tx.bumpWriteSeq(ctx)
}

// UpdateLabel changes a membership's liveness under compare-and-swap. Removing a
// label is a tombstone, so re-adding it later revives the same record rather
// than losing the history of the removal.
func (tx *Tx) UpdateLabel(ctx context.Context, label model.Label, observed model.Revision) error {
	if err := label.Validate(); err != nil {
		return err
	}
	const statement = `UPDATE labels
	                   SET tombstoned = ?, revision_generation = ?, revision_replica = ?
	                   WHERE issue_id = ? AND label = ?
	                     AND revision_generation = ? AND revision_replica = ?`
	result, err := tx.ex.ExecContext(ctx, statement,
		tombstoneValue(label.Tombstone),
		label.Revision.Generation,
		string(label.Revision.Replica),
		string(label.IssueID),
		label.Name,
		observed.Generation,
		string(observed.Replica),
	)
	if err != nil {
		return classify("update label", err)
	}
	changed, err := affected(result, "update label")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.resolveMiss(ctx, fmt.Sprintf("label %q on Issue %s", label.Name, label.IssueID),
			`SELECT count(*) FROM labels WHERE issue_id = ? AND label = ?`,
			string(label.IssueID), label.Name)
	}
	return tx.bumpWriteSeq(ctx)
}
