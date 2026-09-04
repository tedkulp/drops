package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/tedkulp/drops/internal/model"
)

// An Issue's creation origin lives in its ID reservation, not on the Issue row:
// the reservation is immutable and outlives every revision of the Issue, so
// duplicating it would give one fact two homes. Reads join it back on.
const issueInsertColumns = `id, project_key, title, description, issue_type, status, priority,
                            assignee, close_reason, deferred_until, created_at, updated_at,
                            closed_at, started_at, tombstoned,
                            revision_generation, revision_replica`

const issueSelect = `SELECT issues.id, issues.project_key, issues.title, issues.description,
                            issues.issue_type, issues.status, issues.priority, issues.assignee,
                            issues.close_reason, issues.deferred_until, issues.created_at,
                            issues.updated_at, issues.closed_at, issues.started_at,
                            issues.tombstoned, owner.creation_replica,
                            issues.revision_generation, issues.revision_replica
                     FROM issues
                     JOIN id_owners AS owner ON owner.id = issues.id AND owner.kind = 'issue'`

func scanIssue(row scanner) (model.Issue, error) {
	var (
		issue         model.Issue
		assignee      sql.NullString
		closeReason   sql.NullString
		deferredUntil sql.NullString
		closedAt      sql.NullString
		startedAt     sql.NullString
		tombstoned    int
	)
	err := row.Scan(
		&issue.ID,
		&issue.ProjectKey,
		&issue.Title,
		&issue.Description,
		&issue.Type,
		&issue.Status,
		&issue.Priority,
		&assignee,
		&closeReason,
		&deferredUntil,
		&issue.CreatedAt,
		&issue.UpdatedAt,
		&closedAt,
		&startedAt,
		&tombstoned,
		&issue.CreationReplica,
		&issue.Revision.Generation,
		&issue.Revision.Replica,
	)
	if err != nil {
		return model.Issue{}, err
	}
	issue.Assignee = textPtr(assignee)
	issue.CloseReason = textPtr(closeReason)
	issue.DeferredUntil = stampPtr(deferredUntil)
	issue.ClosedAt = stampPtr(closedAt)
	issue.StartedAt = stampPtr(startedAt)
	issue.Tombstone = tombstoneFrom(tombstoned)
	return issue, nil
}

// Issue reads one Issue, tombstoned or not. A tombstone is a state, not an
// absence: the row is still addressable and still exports.
func (read reader) Issue(ctx context.Context, id model.ID) (model.Issue, error) {
	row := read.ex.QueryRowContext(ctx, issueSelect+` WHERE issues.id = ?`, string(id))
	issue, err := scanIssue(row)
	if err != nil {
		return model.Issue{}, wrapNotFound(fmt.Sprintf("Issue %s", id), err)
	}
	return issue, nil
}

// IssuesByID reads the named Issues in ONE query, tombstoned included. It
// mirrors Issue rather than a listing, so an id naming no Issue is ErrNotFound
// and not an absent key: a caller asks by ids it read off relation records
// whose foreign keys guarantee the row, and a miss there is a fault. Duplicate
// ids collapse, and the empty request runs no query at all.
func (read reader) IssuesByID(ctx context.Context, ids []model.ID) (map[model.ID]model.Issue, error) {
	wanted := make([]model.ID, 0, len(ids))
	asked := make(map[model.ID]bool, len(ids))
	for _, id := range ids {
		if asked[id] {
			continue
		}
		asked[id] = true
		wanted = append(wanted, id)
	}
	found := make(map[model.ID]model.Issue, len(wanted))
	if len(wanted) == 0 {
		return found, nil
	}

	placeholders, args := inList(wanted, func(id model.ID) any { return string(id) })
	rows, err := read.ex.QueryContext(ctx, issueSelect+` WHERE issues.id IN `+placeholders, args...)
	issues, err := collect(rows, err, "read Issues by ID", scanIssue)
	if err != nil {
		return nil, err
	}
	for _, issue := range issues {
		found[issue.ID] = issue
	}
	for _, id := range wanted {
		if _, ok := found[id]; !ok {
			return nil, fmt.Errorf("%w: Issue %s", model.ErrNotFound, id)
		}
	}
	return found, nil
}

// IssueFilter selects Issues. A zero filter selects every live Issue in every
// Project. Whether an Issue is blocked, ready or deferred is derived from other
// records and is not asked here.
type IssueFilter struct {
	// Project scopes to one Project. Nil spans the whole store.
	Project *model.ProjectKey
	// Statuses, Types and Priorities each select any of the listed values;
	// empty means no constraint.
	Statuses   []model.Status
	Types      []model.IssueType
	Priorities []int
	// AnyLabels selects Issues carrying at least one of these live labels.
	AnyLabels []string
	// Assignee selects one assignee. The empty string selects unassigned
	// Issues, which is a different question from "any assignee".
	Assignee *string
	// IncludeTombstoned adds removed Issues to the result.
	IncludeTombstoned bool
	// Limit caps the result; zero or less means every match.
	Limit int
}

// Issues lists matching Issues in queue order: priority first, then newest
// first, then ID, which is the order idx_issues_project_queue is built for.
func (read reader) Issues(ctx context.Context, filter IssueFilter) ([]model.Issue, error) {
	return read.issuesWhere(ctx, filter, "")
}

// issuesWhere is the one place an Issue listing is built. Search adds its match
// predicate here rather than assembling a second query, so a filtered search and
// a filtered list can never disagree about what a filter means.
func (read reader) issuesWhere(ctx context.Context, filter IssueFilter, extra string, extraArgs ...any) ([]model.Issue, error) {
	predicates, args := filter.predicates()
	if extra != "" {
		predicates = append(predicates, extra)
		args = append(args, extraArgs...)
	}

	query := issueSelect
	if len(predicates) > 0 {
		query += " WHERE " + strings.Join(predicates, " AND ")
	}
	query += " ORDER BY issues.priority, issues.created_at DESC, issues.id"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := read.ex.QueryContext(ctx, query, args...)
	return collect(rows, err, "list Issues", scanIssue)
}

func (filter IssueFilter) predicates() ([]string, []any) {
	var (
		predicates []string
		args       []any
	)
	if !filter.IncludeTombstoned {
		predicates = append(predicates, "issues.tombstoned = 0")
	}
	if filter.Project != nil {
		predicates = append(predicates, "issues.project_key = ?")
		args = append(args, string(*filter.Project))
	}
	if len(filter.Statuses) > 0 {
		placeholders, values := inList(filter.Statuses, func(status model.Status) any { return string(status) })
		predicates = append(predicates, "issues.status IN "+placeholders)
		args = append(args, values...)
	}
	if len(filter.Types) > 0 {
		placeholders, values := inList(filter.Types, func(kind model.IssueType) any { return string(kind) })
		predicates = append(predicates, "issues.issue_type IN "+placeholders)
		args = append(args, values...)
	}
	if len(filter.Priorities) > 0 {
		placeholders, values := inList(filter.Priorities, func(priority int) any { return priority })
		predicates = append(predicates, "issues.priority IN "+placeholders)
		args = append(args, values...)
	}
	if filter.Assignee != nil {
		if *filter.Assignee == "" {
			predicates = append(predicates, "(issues.assignee IS NULL OR issues.assignee = '')")
		} else {
			predicates = append(predicates, "issues.assignee = ?")
			args = append(args, *filter.Assignee)
		}
	}
	if len(filter.AnyLabels) > 0 {
		placeholders, values := inList(filter.AnyLabels, func(label string) any { return label })
		predicates = append(predicates,
			"EXISTS (SELECT 1 FROM labels WHERE labels.issue_id = issues.id"+
				" AND labels.tombstoned = 0 AND labels.label IN "+placeholders+")")
		args = append(args, values...)
	}
	return predicates, args
}

// PutIssue inserts an Issue whose ID is already reserved to it. An unreserved ID
// or a missing Project is ErrNotFound, a taken ID is ErrConflict, and a creation
// replica that disagrees with the reservation is ErrInvalid: the reservation is
// the authority, so an Issue that would read back differently is never written.
func (tx *Tx) PutIssue(ctx context.Context, issue model.Issue) error {
	if err := issue.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO issues (` + issueInsertColumns + `)
	                   SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
	                   WHERE EXISTS (SELECT 1 FROM id_owners
	                                 WHERE id = ? AND kind = 'issue' AND creation_replica = ?)`
	result, err := tx.ex.ExecContext(ctx, statement,
		string(issue.ID),
		string(issue.ProjectKey),
		issue.Title,
		issue.Description,
		string(issue.Type),
		string(issue.Status),
		issue.Priority,
		nullText(issue.Assignee),
		nullText(issue.CloseReason),
		nullStamp(issue.DeferredUntil),
		string(issue.CreatedAt),
		string(issue.UpdatedAt),
		nullStamp(issue.ClosedAt),
		nullStamp(issue.StartedAt),
		tombstoneValue(issue.Tombstone),
		issue.Revision.Generation,
		string(issue.Revision.Replica),
		string(issue.ID),
		string(issue.CreationReplica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert Issue %s", issue.ID), err)
	}
	changed, err := affected(result, "insert Issue")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.explainReservation(ctx, issue.ID, model.OwnerIssue, issue.CreationReplica)
	}
	return tx.bumpWriteSeq(ctx)
}

// explainReservation says why an insert guarded by an ID reservation wrote
// nothing: either no reservation exists, or it names a different creation origin.
func (tx *Tx) explainReservation(ctx context.Context, id model.ID, kind model.OwnerKind, replica model.ReplicaKey) error {
	owner, err := tx.IDOwner(ctx, id)
	if err != nil {
		return err
	}
	if owner.Kind != kind {
		return fmt.Errorf("%w: ID %s is reserved to a %s", model.ErrConflict, id, owner.Kind)
	}
	return fmt.Errorf("%w: %s %s was created by Replica %s, not %s",
		model.ErrInvalid, kind, id, owner.CreationReplica, replica)
}

// UpdateIssue replaces an Issue whose stored revision is still observed. ID and
// creation origin are immutable and are never written by an update.
func (tx *Tx) UpdateIssue(ctx context.Context, issue model.Issue, observed model.Revision) error {
	if err := issue.Validate(); err != nil {
		return err
	}
	const statement = `UPDATE issues
	                   SET project_key = ?, title = ?, description = ?, issue_type = ?,
	                       status = ?, priority = ?, assignee = ?, close_reason = ?,
	                       deferred_until = ?, created_at = ?, updated_at = ?,
	                       closed_at = ?, started_at = ?, tombstoned = ?,
	                       revision_generation = ?, revision_replica = ?
	                   WHERE id = ?
	                     AND revision_generation = ? AND revision_replica = ?`
	result, err := tx.ex.ExecContext(ctx, statement,
		string(issue.ProjectKey),
		issue.Title,
		issue.Description,
		string(issue.Type),
		string(issue.Status),
		issue.Priority,
		nullText(issue.Assignee),
		nullText(issue.CloseReason),
		nullStamp(issue.DeferredUntil),
		string(issue.CreatedAt),
		string(issue.UpdatedAt),
		nullStamp(issue.ClosedAt),
		nullStamp(issue.StartedAt),
		tombstoneValue(issue.Tombstone),
		issue.Revision.Generation,
		string(issue.Revision.Replica),
		string(issue.ID),
		observed.Generation,
		string(observed.Replica),
	)
	if err != nil {
		return classify(fmt.Sprintf("update Issue %s", issue.ID), err)
	}
	changed, err := affected(result, "update Issue")
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.resolveMiss(ctx, fmt.Sprintf("Issue %s", issue.ID),
			`SELECT count(*) FROM issues WHERE id = ?`, string(issue.ID))
	}
	return tx.bumpWriteSeq(ctx)
}

// inList renders a placeholder list and its arguments for an IN clause. Values
// are never interpolated into SQL text.
func inList[T any](values []T, convert func(T) any) (string, []any) {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, convert(value))
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(values)), ",") + ")", args
}
