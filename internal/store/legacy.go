package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tedkulp/drops/internal/model"
)

// The legacy rewrite is a one-time conversion of the single v6 store this build
// replaces. It is not a migration framework: v7 is created directly for a fresh
// store, and migrations 1 through 6 do not ship. An exact v6 shape is the only
// thing Rewrite accepts, so a database that has drifted is refused rather than
// converted on a guess.

// legacyTables is the measured v6 shape: every base table, with its columns in
// declaration order. A store that does not match exactly is not the store this
// conversion was written against.
var legacyTables = map[string][]string{
	"projects": {"id", "slug", "repo_path", "remote_url", "archived_at", "created_at"},
	"issues": {"id", "project_id", "title", "description", "issue_type", "status", "priority",
		"assignee", "close_reason", "deferred_until", "created_at", "updated_at", "closed_at",
		"started_at", "metadata"},
	"dependencies": {"from_id", "to_id", "dep_type", "created_at"},
	"labels":       {"issue_id", "label"},
	"comments":     {"id", "issue_id", "author", "body", "created_at"},
	"memories": {"id", "project_id", "kind", "title", "body", "ambient", "salience",
		"source_project", "source", "created_at", "updated_at", "last_recalled_at",
		"recall_count", "deleted_at", "superseded_by"},
	"sync_state": {"id", "dirty_at", "last_export_at", "last_commit_at", "last_commit_sha",
		"last_dolt_warn_at", "last_export_ms", "last_sync_unlocked"},
}

// LegacyMemory is one v6 memory row, before any curation. Every field the old
// schema carried is present, including the ones v7 drops, so the caller's
// judgement is made on the whole record rather than on what survived.
type LegacyMemory struct {
	ID            model.ID
	ProjectSlug   string // empty when the legacy row had no project
	Kind          string
	Title         string
	Body          string
	Ambient       bool
	Salience      int
	SourceProject string
	Source        string
	CreatedAt     model.Timestamp
	UpdatedAt     model.Timestamp
	SupersededBy  *model.ID
	Deleted       bool
}

// MemoryPlan is what one legacy memory becomes. Identity and lifecycle
// timestamps are not settable: an ID is permanent and a migration is not a
// modification, so they carry across untouched.
type MemoryPlan struct {
	// ProjectSlug names the owning Project, which is created if the legacy
	// store had no such Project.
	ProjectSlug  string
	Title        string
	Body         string
	Provenance   *string
	SupersededBy *model.ID
}

// LegacyPlan carries the decisions the conversion cannot derive from the data.
type LegacyPlan struct {
	// Replica is the cutover Replica: creation origin and generation-1 revision
	// for every converted record.
	Replica model.ReplicaKey
	// Projects pins legacy slugs to Project keys. A slug that is absent is
	// minted, except the reserved slugs, which always take their fixed keys.
	Projects map[string]model.ProjectKey
	// Locators maps a legacy slug to the normalized repository locator its
	// remote_url becomes. A remote_url with no mapping is reported, not guessed:
	// normalizing a Git remote is a rule, and rules do not live here.
	Locators map[string]string
	// Memories curates the notebook. It returns what one legacy memory becomes
	// and whether to keep it at all. Nil keeps every memory, under its legacy
	// scope, with its title, body and source carried across.
	Memories func(LegacyMemory) (MemoryPlan, bool)
	// Expect asserts the measured outcome. A mismatch rolls the whole
	// conversion back, so the authoritative cutover cannot half-succeed.
	Expect *LegacyCounts
}

// LegacyCounts is what a conversion wrote.
type LegacyCounts struct {
	Projects        int
	Issues          int
	IssueParents    int
	OmittedParents  int
	Dependencies    int
	Labels          int
	Comments        int
	Memories        int
	OmittedMemories int
}

// LegacyReport is a conversion's full account of itself.
type LegacyReport struct {
	LegacyCounts

	// OmittedParents names the children whose only parentage evidence was
	// cross-Project ID spelling on a removed Issue.
	OmittedParents []model.ID
	// OmittedMemories names the memories the plan dropped.
	OmittedMemories []model.ID
	// SkippedRemotes names Projects whose remote_url had no locator mapping.
	SkippedRemotes []string
	// ProjectKeys is the slug-to-key mapping the conversion used, including
	// every key it minted.
	ProjectKeys map[string]model.ProjectKey
}

// Rewrite converts an exact legacy v6 store at path into v7, in place and in one
// transaction. It refuses any other shape, and a refusal writes nothing.
func Rewrite(ctx context.Context, path string, plan LegacyPlan) (LegacyReport, error) {
	if err := guardPath(path); err != nil {
		return LegacyReport{}, err
	}
	if err := plan.Replica.Validate(); err != nil {
		return LegacyReport{}, fmt.Errorf("cutover replica: %w", err)
	}
	if _, err := os.Stat(path); err != nil {
		return LegacyReport{}, fmt.Errorf("open legacy store: %w", err)
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return LegacyReport{}, fmt.Errorf("open legacy store: %w", err)
	}
	defer db.Close()

	// One pinned connection for the whole conversion. PRAGMA foreign_keys is a
	// no-op inside a transaction and is per-connection state, so it has to be
	// set here, before BEGIN, on the connection the transaction will run on.
	// Dropping the v6 tables with enforcement on would cascade deletes through
	// rows the conversion is still reading.
	conn, err := db.Conn(ctx)
	if err != nil {
		return LegacyReport{}, fmt.Errorf("pin conversion connection: %w", err)
	}
	defer conn.Close()

	if err := verifyLegacyShape(ctx, conn, path); err != nil {
		return LegacyReport{}, err
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return LegacyReport{}, fmt.Errorf("relax foreign keys for conversion: %w", err)
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "PRAGMA foreign_keys = ON")

	source, err := readLegacy(ctx, conn)
	if err != nil {
		return LegacyReport{}, err
	}

	sqlTx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return LegacyReport{}, fmt.Errorf("begin conversion: %w", err)
	}
	defer sqlTx.Rollback()

	report, err := source.write(ctx, &Tx{reader: reader{ex: sqlTx}, tx: sqlTx}, plan)
	if err != nil {
		return LegacyReport{}, err
	}
	if err := checkConversion(ctx, sqlTx, report, plan.Expect); err != nil {
		return LegacyReport{}, err
	}
	if err := sqlTx.Commit(); err != nil {
		return LegacyReport{}, fmt.Errorf("commit conversion: %w", err)
	}
	return report, nil
}

// verifyLegacyShape refuses anything but the one v6 store this conversion was
// written against: the right schema version, exactly the expected tables, and
// exactly the expected columns in each.
func verifyLegacyShape(ctx context.Context, conn *sql.Conn, path string) error {
	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version != LegacySchemaVersion {
		return fmt.Errorf("%w: database at %s reports schema version %d, want %d",
			ErrUnsupportedSchema, path, version, LegacySchemaVersion)
	}

	rows, err := conn.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table'
		   AND name NOT LIKE 'sqlite_%' AND name NOT LIKE '%_fts%' ORDER BY name`)
	if err != nil {
		return fmt.Errorf("inspect legacy schema: %w", err)
	}
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("inspect legacy schema: %w", err)
		}
		found[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect legacy schema: %w", err)
	}

	for name := range found {
		if _, expected := legacyTables[name]; !expected {
			return fmt.Errorf("%w: legacy store carries unexpected table %q", ErrUnsupportedSchema, name)
		}
	}
	for name, columns := range legacyTables {
		if !found[name] {
			return fmt.Errorf("%w: legacy store is missing table %q", ErrUnsupportedSchema, name)
		}
		actual, err := tableColumns(ctx, conn, name)
		if err != nil {
			return err
		}
		if strings.Join(actual, ",") != strings.Join(columns, ",") {
			return fmt.Errorf("%w: legacy table %q has columns [%s], want [%s]",
				ErrUnsupportedSchema, name, strings.Join(actual, ", "), strings.Join(columns, ", "))
		}
	}
	return nil
}

func tableColumns(ctx context.Context, conn *sql.Conn, table string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, fmt.Errorf("inspect legacy table %q: %w", table, err)
	}
	defer rows.Close()

	columns := []string{}
	for rows.Next() {
		var (
			index               int
			name, declared      string
			notNull, primaryKey int
			defaultValue        sql.NullString
		)
		if err := rows.Scan(&index, &name, &declared, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("inspect legacy table %q: %w", table, err)
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

// legacyStore is the whole v6 store, read before anything is dropped.
type legacyStore struct {
	projects     []legacyProject
	issues       []legacyIssue
	dependencies []legacyDependency
	labels       []legacyLabel
	comments     []legacyComment
	memories     []LegacyMemory

	projectSlugs map[int64]string
	issueIndex   map[model.ID]*legacyIssue
}

type legacyProject struct {
	id         int64
	slug       string
	remoteURL  string
	archivedAt *model.Timestamp
	createdAt  model.Timestamp
}

type legacyIssue struct {
	id            model.ID
	projectID     int64
	title         string
	description   string
	issueType     model.IssueType
	status        string
	priority      int
	assignee      *string
	closeReason   *string
	deferredUntil *model.Timestamp
	createdAt     model.Timestamp
	updatedAt     model.Timestamp
	closedAt      *model.Timestamp
	startedAt     *model.Timestamp
}

type legacyDependency struct {
	from      model.ID
	to        model.ID
	depType   string
	createdAt model.Timestamp
}

type legacyLabel struct {
	issue model.ID
	name  string
}

type legacyComment struct {
	id        model.CommentID
	issue     model.ID
	author    string
	body      string
	createdAt model.Timestamp
}

func readLegacy(ctx context.Context, conn *sql.Conn) (*legacyStore, error) {
	source := &legacyStore{
		projectSlugs: map[int64]string{},
		issueIndex:   map[model.ID]*legacyIssue{},
	}

	rows, err := conn.QueryContext(ctx,
		`SELECT id, slug, coalesce(remote_url, ''), archived_at, created_at
		 FROM projects ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy projects: %w", err)
	}
	for rows.Next() {
		var (
			project    legacyProject
			archivedAt sql.NullString
		)
		if err := rows.Scan(&project.id, &project.slug, &project.remoteURL,
			&archivedAt, &project.createdAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read legacy projects: %w", err)
		}
		project.archivedAt = stampPtr(archivedAt)
		source.projects = append(source.projects, project)
		source.projectSlugs[project.id] = project.slug
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy projects: %w", err)
	}

	rows, err = conn.QueryContext(ctx,
		`SELECT id, project_id, title, description, issue_type, status, priority,
		        assignee, close_reason, deferred_until, created_at, updated_at,
		        closed_at, started_at
		 FROM issues ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy issues: %w", err)
	}
	for rows.Next() {
		var (
			issue                                                     legacyIssue
			assignee, closeReason, deferredUntil, closedAt, startedAt sql.NullString
		)
		if err := rows.Scan(&issue.id, &issue.projectID, &issue.title, &issue.description,
			&issue.issueType, &issue.status, &issue.priority, &assignee, &closeReason,
			&deferredUntil, &issue.createdAt, &issue.updatedAt, &closedAt, &startedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read legacy issues: %w", err)
		}
		issue.assignee = textPtr(assignee)
		issue.closeReason = textPtr(closeReason)
		issue.deferredUntil = stampPtr(deferredUntil)
		issue.closedAt = stampPtr(closedAt)
		issue.startedAt = stampPtr(startedAt)
		source.issues = append(source.issues, issue)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy issues: %w", err)
	}
	for index := range source.issues {
		source.issueIndex[source.issues[index].id] = &source.issues[index]
	}

	rows, err = conn.QueryContext(ctx,
		`SELECT from_id, to_id, dep_type, created_at FROM dependencies
		 ORDER BY from_id, to_id, dep_type`)
	if err != nil {
		return nil, fmt.Errorf("read legacy dependencies: %w", err)
	}
	for rows.Next() {
		var dependency legacyDependency
		if err := rows.Scan(&dependency.from, &dependency.to,
			&dependency.depType, &dependency.createdAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read legacy dependencies: %w", err)
		}
		source.dependencies = append(source.dependencies, dependency)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy dependencies: %w", err)
	}

	rows, err = conn.QueryContext(ctx, `SELECT issue_id, label FROM labels ORDER BY issue_id, label`)
	if err != nil {
		return nil, fmt.Errorf("read legacy labels: %w", err)
	}
	for rows.Next() {
		var label legacyLabel
		if err := rows.Scan(&label.issue, &label.name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read legacy labels: %w", err)
		}
		source.labels = append(source.labels, label)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy labels: %w", err)
	}

	rows, err = conn.QueryContext(ctx,
		`SELECT id, issue_id, author, body, created_at FROM comments ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy comments: %w", err)
	}
	for rows.Next() {
		var comment legacyComment
		if err := rows.Scan(&comment.id, &comment.issue, &comment.author,
			&comment.body, &comment.createdAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read legacy comments: %w", err)
		}
		source.comments = append(source.comments, comment)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read legacy comments: %w", err)
	}

	rows, err = conn.QueryContext(ctx,
		`SELECT id, project_id, kind, title, body, ambient, salience, source_project,
		        source, created_at, updated_at, superseded_by, deleted_at
		 FROM memories ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy memories: %w", err)
	}
	for rows.Next() {
		var (
			memory       LegacyMemory
			projectID    sql.NullInt64
			ambient      int
			supersededBy sql.NullString
			deletedAt    sql.NullString
		)
		if err := rows.Scan(&memory.ID, &projectID, &memory.Kind, &memory.Title, &memory.Body,
			&ambient, &memory.Salience, &memory.SourceProject, &memory.Source,
			&memory.CreatedAt, &memory.UpdatedAt, &supersededBy, &deletedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read legacy memories: %w", err)
		}
		if projectID.Valid {
			memory.ProjectSlug = source.projectSlugs[projectID.Int64]
		}
		memory.Ambient = ambient != 0
		memory.SupersededBy = idPtr(supersededBy)
		memory.Deleted = deletedAt.Valid
		source.memories = append(source.memories, memory)
	}
	rows.Close()
	return source, rows.Err()
}

// write drops the v6 shape, creates v7, and inserts the converted records.
func (source *legacyStore) write(ctx context.Context, tx *Tx, plan LegacyPlan) (LegacyReport, error) {
	if err := dropLegacySchema(ctx, tx); err != nil {
		return LegacyReport{}, err
	}
	if _, err := tx.ex.ExecContext(ctx, schemaDDL); err != nil {
		return LegacyReport{}, fmt.Errorf("create v7 schema: %w", err)
	}

	report := LegacyReport{ProjectKeys: map[string]model.ProjectKey{}}
	keys := &projectKeys{plan: plan, assigned: report.ProjectKeys}

	if err := source.writeProjects(ctx, tx, plan, keys, &report); err != nil {
		return LegacyReport{}, err
	}
	if err := source.writeIssues(ctx, tx, plan, keys, &report); err != nil {
		return LegacyReport{}, err
	}
	if err := source.writeParentage(ctx, tx, plan, &report); err != nil {
		return LegacyReport{}, err
	}
	if err := source.writeRelations(ctx, tx, plan, &report); err != nil {
		return LegacyReport{}, err
	}
	if err := source.writeMemories(ctx, tx, plan, keys, &report); err != nil {
		return LegacyReport{}, err
	}
	report.Projects = len(report.ProjectKeys)
	return report, nil
}

// dropLegacySchema removes every v6 object. The virtual tables go with their
// triggers, which is why they are dropped before the content tables they mirror.
func dropLegacySchema(ctx context.Context, tx *Tx) error {
	statements := []string{
		"DROP TRIGGER IF EXISTS issues_ai", "DROP TRIGGER IF EXISTS issues_ad",
		"DROP TRIGGER IF EXISTS issues_au",
		"DROP TRIGGER IF EXISTS comments_ai", "DROP TRIGGER IF EXISTS comments_ad",
		"DROP TRIGGER IF EXISTS comments_au",
		"DROP TRIGGER IF EXISTS memories_ai", "DROP TRIGGER IF EXISTS memories_ad",
		"DROP TRIGGER IF EXISTS memories_au",
		"DROP TABLE IF EXISTS issues_fts", "DROP TABLE IF EXISTS comments_fts",
		"DROP TABLE IF EXISTS memories_fts",
		"DROP TABLE IF EXISTS labels", "DROP TABLE IF EXISTS dependencies",
		"DROP TABLE IF EXISTS comments", "DROP TABLE IF EXISTS memories",
		"DROP TABLE IF EXISTS issues", "DROP TABLE IF EXISTS projects",
		"DROP TABLE IF EXISTS sync_state",
	}
	for _, statement := range statements {
		if _, err := tx.ex.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("drop legacy schema: %w", err)
		}
	}
	return nil
}

// projectKeys resolves a legacy slug to a Project key, minting one where the
// plan is silent. The reserved slugs always take their fixed keys, so
// independently converted stores agree on them.
type projectKeys struct {
	plan     LegacyPlan
	assigned map[string]model.ProjectKey
}

func (keys *projectKeys) forSlug(slug string) (model.ProjectKey, error) {
	if key, ok := keys.assigned[slug]; ok {
		return key, nil
	}
	switch slug {
	case model.GlobalProjectSlug:
		keys.assigned[slug] = model.GlobalProjectKey
		return model.GlobalProjectKey, nil
	case model.InboxProjectSlug:
		keys.assigned[slug] = model.InboxProjectKey
		return model.InboxProjectKey, nil
	}
	if key, ok := keys.plan.Projects[slug]; ok {
		if err := key.Validate(); err != nil {
			return "", fmt.Errorf("Project key for %q: %w", slug, err)
		}
		keys.assigned[slug] = key
		return key, nil
	}
	key, err := model.NewProjectKey()
	if err != nil {
		return "", err
	}
	keys.assigned[slug] = key
	return key, nil
}

func (source *legacyStore) writeProjects(ctx context.Context, tx *Tx, plan LegacyPlan, keys *projectKeys, report *LegacyReport) error {
	revision, err := model.InitialRevision(plan.Replica)
	if err != nil {
		return err
	}
	written := map[string]bool{}

	for _, legacy := range source.projects {
		key, err := keys.forSlug(legacy.slug)
		if err != nil {
			return err
		}
		// updated_at has no legacy source. The record's own creation time is
		// used rather than stamping the conversion's clock onto every row: a
		// migration is not a modification anybody made.
		project := model.Project{
			Key:             key,
			Slug:            legacy.slug,
			ArchivedAt:      legacy.archivedAt,
			CreatedAt:       legacy.createdAt,
			UpdatedAt:       legacy.createdAt,
			CreationReplica: plan.Replica,
			Revision:        revision,
		}
		if err := tx.PutProject(ctx, project); err != nil {
			return err
		}
		written[legacy.slug] = true

		if legacy.remoteURL == "" {
			continue
		}
		locator, mapped := plan.Locators[legacy.slug]
		if !mapped {
			report.SkippedRemotes = append(report.SkippedRemotes, legacy.slug)
			continue
		}
		if err := tx.PutRepositoryLocator(ctx, model.RepositoryLocator{
			ProjectKey: key, Locator: locator, Revision: revision,
		}); err != nil {
			return err
		}
	}

	// The reserved Projects exist in v7 whether or not v6 had them: a memory
	// with no scope lands in global, and unresolved work lands in inbox.
	for _, slug := range []string{model.GlobalProjectSlug, model.InboxProjectSlug} {
		if written[slug] {
			continue
		}
		key, err := keys.forSlug(slug)
		if err != nil {
			return err
		}
		if err := tx.PutProject(ctx, model.Project{
			Key:             key,
			Slug:            slug,
			CreatedAt:       conversionStamp(source),
			UpdatedAt:       conversionStamp(source),
			CreationReplica: plan.Replica,
			Revision:        revision,
		}); err != nil {
			return err
		}
	}
	return nil
}

// conversionStamp is the timestamp a record invented by the conversion carries:
// the oldest thing in the store, so a created Project never looks newer than the
// data it holds. It avoids reading a clock, which would make the conversion
// non-reproducible.
func conversionStamp(source *legacyStore) model.Timestamp {
	oldest := model.Timestamp("")
	for _, project := range source.projects {
		if oldest == "" || project.createdAt < oldest {
			oldest = project.createdAt
		}
	}
	if oldest == "" {
		return model.Timestamp("1970-01-01T00:00:00Z")
	}
	return oldest
}

func (source *legacyStore) writeIssues(ctx context.Context, tx *Tx, plan LegacyPlan, keys *projectKeys, report *LegacyReport) error {
	revision, err := model.InitialRevision(plan.Replica)
	if err != nil {
		return err
	}
	for _, legacy := range source.issues {
		slug, known := source.projectSlugs[legacy.projectID]
		if !known {
			return fmt.Errorf("%w: Issue %s names legacy project %d, which does not exist",
				model.ErrInvalid, legacy.id, legacy.projectID)
		}
		key, err := keys.forSlug(slug)
		if err != nil {
			return err
		}
		status, tombstone, err := convertStatus(legacy)
		if err != nil {
			return err
		}
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID: legacy.id, Kind: model.OwnerIssue, CreationReplica: plan.Replica,
		}); err != nil {
			return err
		}
		if err := tx.PutIssue(ctx, model.Issue{
			ID:              legacy.id,
			ProjectKey:      key,
			Title:           legacy.title,
			Description:     legacy.description,
			Type:            legacy.issueType,
			Status:          status,
			Priority:        legacy.priority,
			Assignee:        legacy.assignee,
			CloseReason:     legacy.closeReason,
			DeferredUntil:   legacy.deferredUntil,
			CreatedAt:       legacy.createdAt,
			UpdatedAt:       legacy.updatedAt,
			ClosedAt:        legacy.closedAt,
			StartedAt:       legacy.startedAt,
			Tombstone:       tombstone,
			CreationReplica: plan.Replica,
			Revision:        revision,
		}); err != nil {
			return err
		}
		report.Issues++
	}
	return nil
}

// convertStatus maps a v6 lifecycle onto the v7 pair of status and tombstone.
//
// "deleted" was never a lifecycle: it is a removal, so it becomes a tombstone
// over the lifecycle the Issue actually reached — closed where a closed_at says
// it was closed, open otherwise. "blocked" was a lifecycle that v7 derives from
// unfinished blocking relationships instead; the legacy store has no such row,
// so meeting one means this is not the store the conversion was written for.
func convertStatus(legacy legacyIssue) (model.Status, model.Tombstone, error) {
	switch legacy.status {
	case "open", "in_progress", "closed":
		return model.Status(legacy.status), model.Live, nil
	case "deleted":
		if legacy.closedAt != nil {
			return model.StatusClosed, model.Tombstoned, nil
		}
		return model.StatusOpen, model.Tombstoned, nil
	default:
		return "", model.Live, fmt.Errorf("%w: Issue %s carries legacy status %q",
			ErrUnsupportedSchema, legacy.id, legacy.status)
	}
}

// writeParentage reconciles the two sources of v6 parentage into the one
// authoritative record per child.
//
// An explicit parent-child edge is authoritative wherever it exists, including
// across Projects. Dotted ID spelling is admitted as evidence only where no edge
// exists and both ends sit in one Project. A cross-Project dotted-only pair is
// the signature of the legacy create --parent bug, which minted children in the
// wrong Project: it is dropped when the child was removed, and refuses the
// conversion when it is still live, because then it is genuinely ambiguous.
func (source *legacyStore) writeParentage(ctx context.Context, tx *Tx, plan LegacyPlan, report *LegacyReport) error {
	revision, err := model.InitialRevision(plan.Replica)
	if err != nil {
		return err
	}

	explicit := map[model.ID]legacyDependency{}
	for _, dependency := range source.dependencies {
		if dependency.depType != "parent-child" {
			continue
		}
		if existing, seen := explicit[dependency.from]; seen && existing.to != dependency.to {
			return fmt.Errorf("%w: Issue %s has explicit parents %s and %s",
				model.ErrInvalid, dependency.from, existing.to, dependency.to)
		}
		explicit[dependency.from] = dependency
	}

	type relation struct {
		parent    model.ID
		createdAt model.Timestamp
	}
	retained := map[model.ID]relation{}

	for child, edge := range explicit {
		if _, ok := source.issueIndex[child]; !ok {
			return fmt.Errorf("%w: parent edge names missing Issue %s", model.ErrNotFound, child)
		}
		if _, ok := source.issueIndex[edge.to]; !ok {
			return fmt.Errorf("%w: parent edge names missing Issue %s", model.ErrNotFound, edge.to)
		}
		if spelled, ok := source.dottedParent(child); ok && spelled != edge.to {
			return fmt.Errorf("%w: Issue %s names parent %s but is spelled a child of %s",
				model.ErrInvalid, child, edge.to, spelled)
		}
		retained[child] = relation{parent: edge.to, createdAt: edge.createdAt}
	}

	for index := range source.issues {
		child := &source.issues[index]
		if _, hasEdge := explicit[child.id]; hasEdge {
			continue
		}
		parent, spelled := source.dottedParent(child.id)
		if !spelled {
			continue
		}
		if source.issueIndex[parent].projectID == child.projectID {
			// Minting the child's ID is the only defensible creation event for
			// a relation that was never written down.
			retained[child.id] = relation{parent: parent, createdAt: child.createdAt}
			continue
		}
		if child.status != "deleted" {
			return fmt.Errorf(
				"%w: live Issue %s is spelled a child of %s in another Project, with no explicit edge",
				model.ErrInvalid, child.id, parent)
		}
		report.OmittedParents = append(report.OmittedParents, child.id)
	}

	children := make([]model.ID, 0, len(retained))
	for child := range retained {
		children = append(children, child)
	}
	sort.Slice(children, func(left, right int) bool { return children[left] < children[right] })

	if err := checkAcyclic(children, func(child model.ID) (model.ID, bool) {
		record, ok := retained[child]
		return record.parent, ok
	}); err != nil {
		return err
	}
	for _, child := range children {
		record := retained[child]
		if err := tx.PutIssueParent(ctx, model.IssueParent{
			ChildID:   child,
			ParentID:  record.parent,
			CreatedAt: record.createdAt,
			Revision:  revision,
		}); err != nil {
			return err
		}
		report.IssueParents++
	}
	sort.Slice(report.OmittedParents, func(left, right int) bool {
		return report.OmittedParents[left] < report.OmittedParents[right]
	})
	report.LegacyCounts.OmittedParents = len(report.OmittedParents)
	return nil
}

// dottedParent reads the parent an ID spells, if that Issue exists. This is the
// last time ID spelling means anything: after the conversion, issue_parents is
// the only authority.
func (source *legacyStore) dottedParent(child model.ID) (model.ID, bool) {
	text := string(child)
	dot := strings.LastIndexByte(text, '.')
	if dot <= 0 {
		return "", false
	}
	parent := model.ID(text[:dot])
	if _, ok := source.issueIndex[parent]; !ok {
		return "", false
	}
	return parent, true
}

// checkAcyclic refuses a parentage graph with a cycle, which no sequence of
// inserts would catch: every row is individually valid.
func checkAcyclic[T comparable](nodes []T, parentOf func(T) (T, bool)) error {
	const unvisited, visiting, done = 0, 1, 2
	state := map[T]int{}

	var walk func(T) error
	walk = func(node T) error {
		switch state[node] {
		case visiting:
			return fmt.Errorf("%w: parentage cycle through %v", model.ErrInvalid, node)
		case done:
			return nil
		}
		state[node] = visiting
		if parent, ok := parentOf(node); ok {
			if err := walk(parent); err != nil {
				return err
			}
		}
		state[node] = done
		return nil
	}
	for _, node := range nodes {
		if err := walk(node); err != nil {
			return err
		}
	}
	return nil
}

func (source *legacyStore) writeRelations(ctx context.Context, tx *Tx, plan LegacyPlan, report *LegacyReport) error {
	revision, err := model.InitialRevision(plan.Replica)
	if err != nil {
		return err
	}
	for _, legacy := range source.dependencies {
		if legacy.depType == "parent-child" {
			continue
		}
		if err := tx.PutDependency(ctx, model.Dependency{
			FromID:    legacy.from,
			ToID:      legacy.to,
			Type:      model.DependencyType(legacy.depType),
			CreatedAt: legacy.createdAt,
			Revision:  revision,
		}); err != nil {
			return err
		}
		report.Dependencies++
	}
	for _, legacy := range source.labels {
		if err := tx.PutLabel(ctx, model.Label{
			IssueID: legacy.issue, Name: legacy.name, Revision: revision,
		}); err != nil {
			return err
		}
		report.Labels++
	}
	for _, legacy := range source.comments {
		if err := tx.PutComment(ctx, model.Comment{
			ID:              legacy.id,
			IssueID:         legacy.issue,
			Author:          legacy.author,
			Body:            legacy.body,
			CreatedAt:       legacy.createdAt,
			CreationReplica: plan.Replica,
		}); err != nil {
			return err
		}
		report.Comments++
	}
	return nil
}

func (source *legacyStore) writeMemories(ctx context.Context, tx *Tx, plan LegacyPlan, keys *projectKeys, report *LegacyReport) error {
	revision, err := model.InitialRevision(plan.Replica)
	if err != nil {
		return err
	}

	kept := map[model.ID]MemoryPlan{}
	order := []model.ID{}
	for _, legacy := range source.memories {
		curated, keep := curate(plan, legacy)
		if !keep {
			report.OmittedMemories = append(report.OmittedMemories, legacy.ID)
			continue
		}
		kept[legacy.ID] = curated
		order = append(order, legacy.ID)
	}
	report.LegacyCounts.OmittedMemories = len(report.OmittedMemories)

	// A supersession link may only name a retained memory: a dangling pointer
	// into a dropped row is worse than no pointer at all.
	for id, curated := range kept {
		if curated.SupersededBy == nil {
			continue
		}
		if _, retained := kept[*curated.SupersededBy]; !retained {
			return fmt.Errorf("%w: Memory %s is superseded by %s, which the plan drops",
				model.ErrInvalid, id, *curated.SupersededBy)
		}
	}

	// Write the replacements before the records that point at them: the schema
	// enforces the reference, so a superseding memory has to exist first.
	written := map[model.ID]bool{}
	var writeOne func(model.ID) error
	writeOne = func(id model.ID) error {
		if written[id] {
			return nil
		}
		curated := kept[id]
		if curated.SupersededBy != nil {
			if *curated.SupersededBy == id {
				return fmt.Errorf("%w: Memory %s supersedes itself", model.ErrInvalid, id)
			}
			if err := writeOne(*curated.SupersededBy); err != nil {
				return err
			}
		}
		written[id] = true

		legacy := source.memory(id)
		key, err := keys.forSlug(curated.ProjectSlug)
		if err != nil {
			return err
		}
		if err := ensureProject(ctx, tx, key, curated.ProjectSlug, plan, legacy.CreatedAt, revision); err != nil {
			return err
		}
		if err := tx.PutIDOwner(ctx, model.IDOwner{
			ID: id, Kind: model.OwnerMemory, CreationReplica: plan.Replica,
		}); err != nil {
			return err
		}
		if err := tx.PutMemory(ctx, model.Memory{
			ID:              id,
			ProjectKey:      key,
			Title:           curated.Title,
			Body:            curated.Body,
			Provenance:      curated.Provenance,
			CreatedAt:       legacy.CreatedAt,
			UpdatedAt:       legacy.UpdatedAt,
			SupersededBy:    curated.SupersededBy,
			CreationReplica: plan.Replica,
			Revision:        revision,
		}); err != nil {
			return err
		}
		report.Memories++
		return nil
	}
	for _, id := range order {
		if err := writeOne(id); err != nil {
			return err
		}
	}
	return nil
}

// curate applies the plan's judgement to one legacy memory, or carries it across
// unchanged when the plan has none.
func curate(plan LegacyPlan, legacy LegacyMemory) (MemoryPlan, bool) {
	if plan.Memories != nil {
		return plan.Memories(legacy)
	}
	slug := legacy.ProjectSlug
	if slug == "" {
		slug = model.GlobalProjectSlug
	}
	curated := MemoryPlan{
		ProjectSlug:  slug,
		Title:        legacy.Title,
		Body:         legacy.Body,
		SupersededBy: legacy.SupersededBy,
	}
	if legacy.Source != "" {
		source := legacy.Source
		curated.Provenance = &source
	}
	return curated, true
}

func (source *legacyStore) memory(id model.ID) LegacyMemory {
	for _, legacy := range source.memories {
		if legacy.ID == id {
			return legacy
		}
	}
	return LegacyMemory{}
}

// ensureProject creates a Project a curated memory needs and the legacy store
// did not have. The curation may move a memory into a namespace that never
// existed as a v6 project row.
func ensureProject(ctx context.Context, tx *Tx, key model.ProjectKey, slug string, plan LegacyPlan, created model.Timestamp, revision model.Revision) error {
	if _, err := tx.Project(ctx, key); err == nil {
		return nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return err
	}
	return tx.PutProject(ctx, model.Project{
		Key:             key,
		Slug:            slug,
		CreatedAt:       created,
		UpdatedAt:       created,
		CreationReplica: plan.Replica,
		Revision:        revision,
	})
}

// checkConversion runs the structural checks that no single insert could catch,
// then compares the outcome with what the caller measured. Both run before the
// commit, so a conversion that does not match its own plan writes nothing.
func checkConversion(ctx context.Context, tx *sql.Tx, report LegacyReport, expect *LegacyCounts) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("verify conversion: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent, rowID, fkID any
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			return fmt.Errorf("verify conversion: %w", err)
		}
		return fmt.Errorf("verify conversion: %s row %v references missing %v", table, rowID, parent)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("verify conversion: %w", err)
	}

	if expect == nil {
		return nil
	}
	if got := report.LegacyCounts; got != *expect {
		return fmt.Errorf("%w: conversion wrote %+v, expected %+v", model.ErrConflict, got, *expect)
	}
	return nil
}
