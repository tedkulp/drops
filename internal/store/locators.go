package store

import (
	"context"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

const locatorColumns = `project_key, locator, tombstoned, revision_generation, revision_replica`

func scanLocator(row scanner) (model.RepositoryLocator, error) {
	var (
		locator    model.RepositoryLocator
		tombstoned int
	)
	err := row.Scan(
		&locator.ProjectKey,
		&locator.Locator,
		&tombstoned,
		&locator.Revision.Generation,
		&locator.Revision.Replica,
	)
	if err != nil {
		return model.RepositoryLocator{}, err
	}
	locator.Tombstone = tombstoneFrom(tombstoned)
	return locator, nil
}

// RepositoryLocator reads one locator record, live or tombstoned.
func (read reader) RepositoryLocator(ctx context.Context, project model.ProjectKey, locator string) (model.RepositoryLocator, error) {
	row := read.ex.QueryRowContext(ctx,
		`SELECT `+locatorColumns+` FROM repository_locators
		 WHERE project_key = ? AND locator = ?`, string(project), locator)
	found, err := scanLocator(row)
	if err != nil {
		return model.RepositoryLocator{}, wrapNotFound(
			fmt.Sprintf("repository locator %q of Project %s", locator, project), err)
	}
	return found, nil
}

// ProjectsByLocator lists the live locator records naming one repository. More
// than one answer is possible and is not an error here: a locator is evidence
// for association, not identity, so choosing between candidates is a rule above
// this package.
func (read reader) ProjectsByLocator(ctx context.Context, locator string) ([]model.RepositoryLocator, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+locatorColumns+` FROM repository_locators
		 WHERE locator = ? AND tombstoned = 0 ORDER BY project_key`, locator)
	return collect(rows, err, "list Projects by locator", scanLocator)
}

// ProjectLocators lists one Project's live locators.
func (read reader) ProjectLocators(ctx context.Context, project model.ProjectKey) ([]model.RepositoryLocator, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+locatorColumns+` FROM repository_locators
		 WHERE project_key = ? AND tombstoned = 0 ORDER BY locator`, string(project))
	return collect(rows, err, "list Project locators", scanLocator)
}

// RepositoryLocators lists every locator record, tombstoned included, for export.
func (read reader) RepositoryLocators(ctx context.Context) ([]model.RepositoryLocator, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+locatorColumns+` FROM repository_locators ORDER BY project_key, locator`)
	return collect(rows, err, "list repository locators", scanLocator)
}

func (tx *Tx) PutRepositoryLocator(ctx context.Context, locator model.RepositoryLocator) error {
	if err := locator.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO repository_locators (` + locatorColumns + `)
	                   VALUES (?, ?, ?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(locator.ProjectKey),
		locator.Locator,
		tombstoneValue(locator.Tombstone),
		locator.Revision.Generation,
		string(locator.Revision.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert repository locator %q", locator.Locator), err)
	}
	return tx.bumpWriteSeq(ctx)
}

// UpdateRepositoryLocator changes a locator's liveness under compare-and-swap.
func (tx *Tx) UpdateRepositoryLocator(ctx context.Context, locator model.RepositoryLocator, observed model.Revision) error {
	if err := locator.Validate(); err != nil {
		return err
	}
	const statement = `UPDATE repository_locators
	                   SET tombstoned = ?, revision_generation = ?, revision_replica = ?
	                   WHERE project_key = ? AND locator = ?
	                     AND revision_generation = ? AND revision_replica = ?`
	result, err := tx.ex.ExecContext(ctx, statement,
		tombstoneValue(locator.Tombstone),
		locator.Revision.Generation,
		string(locator.Revision.Replica),
		string(locator.ProjectKey),
		locator.Locator,
		observed.Generation,
		string(observed.Replica),
	)
	if err != nil {
		return classify("update repository locator", err)
	}
	changed, err := affected(result, "update repository locator")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.resolveMiss(ctx,
			fmt.Sprintf("repository locator %q of Project %s", locator.Locator, locator.ProjectKey),
			`SELECT count(*) FROM repository_locators WHERE project_key = ? AND locator = ?`,
			string(locator.ProjectKey), locator.Locator)
	}
	return tx.bumpWriteSeq(ctx)
}
