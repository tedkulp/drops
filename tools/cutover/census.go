package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"

	_ "modernc.org/sqlite"
)

// A Census is what the v6 store says it holds, counted here rather than taken
// from the conversion's own report.
//
// It exists to be handed to store.Rewrite as LegacyPlan.Expect, which rolls the
// whole transaction back on a mismatch. Two independent readings of the same
// rows is a weak check on its own; what makes it worth the code is that the two
// readings share no query, no struct and no traversal, so a conversion that
// silently drops rows disagrees with it. The parentage counts are the strong
// half: they re-derive dw32p.12's rule from its statement instead of from the
// implementation, so agreement is evidence and disagreement is a finding.
type Census struct {
	Counts store.LegacyCounts

	// Slugs are the legacy Project slugs, ordered, for the derived key table.
	Slugs []string
	// Remotes maps a legacy slug to its raw remote_url, for the locator table.
	Remotes map[string]string
	// AmbiguousParents are dotted-only children whose inferred parent is in
	// another Project while the child is live. dw32p.12 refuses these rather
	// than choosing, so finding one stops the cutover before it starts.
	AmbiguousParents []model.ID
}

// TakeCensus reads a v6 store read-only and counts what a conversion should
// write.
func TakeCensus(ctx context.Context, path string) (Census, error) {
	dsn := "file:" + url.PathEscape(path) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return Census{}, fmt.Errorf("open %s for census: %w", path, err)
	}
	defer db.Close()

	slugOf, remotes, err := censusProjects(ctx, db)
	if err != nil {
		return Census{}, err
	}
	census := Census{Remotes: remotes}
	for _, slug := range slugOf {
		census.Slugs = append(census.Slugs, slug)
	}
	sort.Strings(census.Slugs)

	project, err := censusIssues(ctx, db, slugOf)
	if err != nil {
		return Census{}, err
	}
	census.Counts.Issues = len(project)

	if err := censusParents(ctx, db, project, &census); err != nil {
		return Census{}, err
	}
	if err := censusScalars(ctx, db, &census); err != nil {
		return Census{}, err
	}
	owners, err := censusMemories(ctx, db, slugOf, &census)
	if err != nil {
		return Census{}, err
	}

	// Projects is every slug the conversion resolves a key for: the legacy
	// rows, the two reserved slugs it creates whether or not v6 had them, and
	// any namespace the curation moves a memory into.
	namespaces := map[string]bool{model.GlobalProjectSlug: true, model.InboxProjectSlug: true}
	for _, slug := range slugOf {
		namespaces[slug] = true
	}
	for slug := range owners {
		namespaces[slug] = true
	}
	census.Counts.Projects = len(namespaces)
	return census, nil
}

func censusProjects(ctx context.Context, db *sql.DB) (map[int64]string, map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, slug, COALESCE(remote_url, '') FROM projects`)
	if err != nil {
		return nil, nil, fmt.Errorf("census projects: %w", err)
	}
	defer rows.Close()
	slugOf := map[int64]string{}
	remotes := map[string]string{}
	for rows.Next() {
		var (
			id     int64
			slug   string
			remote string
		)
		if err := rows.Scan(&id, &slug, &remote); err != nil {
			return nil, nil, fmt.Errorf("census projects: %w", err)
		}
		slugOf[id] = slug
		if remote != "" {
			remotes[slug] = remote
		}
	}
	return slugOf, remotes, rows.Err()
}

// issueFacts is the little each parentage rule needs about one Issue.
type issueFacts struct {
	slug      string
	tombstone bool
}

func censusIssues(ctx context.Context, db *sql.DB, slugOf map[int64]string) (map[model.ID]issueFacts, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, status FROM issues`)
	if err != nil {
		return nil, fmt.Errorf("census issues: %w", err)
	}
	defer rows.Close()
	facts := map[model.ID]issueFacts{}
	for rows.Next() {
		var (
			id        model.ID
			projectID sql.NullInt64
			status    string
		)
		if err := rows.Scan(&id, &projectID, &status); err != nil {
			return nil, fmt.Errorf("census issues: %w", err)
		}
		facts[id] = issueFacts{slug: slugOf[projectID.Int64], tombstone: status == legacyDeletedStatus}
	}
	return facts, rows.Err()
}

// legacyDeletedStatus is v6's tombstone: a status value rather than a column.
const legacyDeletedStatus = "deleted"

// censusParents re-derives dw32p.12 from its own statement: every explicit
// parent-child edge is authoritative and retained, a dotted-only pair is
// retained when both ends sit in one Project, and a dotted-only pair that
// crosses Projects is an artifact of the legacy `create --parent` bug — omitted
// when its child is tombstoned, and ambiguous, refusing conversion, when it is
// not.
func censusParents(ctx context.Context, db *sql.DB, issues map[model.ID]issueFacts, census *Census) error {
	rows, err := db.QueryContext(ctx,
		`SELECT from_id, to_id, dep_type FROM dependencies`)
	if err != nil {
		return fmt.Errorf("census dependencies: %w", err)
	}
	defer rows.Close()

	explicit := map[model.ID]bool{}
	edges, parents := 0, 0
	for rows.Next() {
		var (
			child, parent model.ID
			depType       string
		)
		if err := rows.Scan(&child, &parent, &depType); err != nil {
			return fmt.Errorf("census dependencies: %w", err)
		}
		edges++
		if depType != legacyParentDep {
			continue
		}
		// from_id is the child: the legacy rows read `<child> | <parent>`.
		explicit[child] = true
		parents++
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for id, child := range issues {
		if explicit[id] {
			continue
		}
		dot := strings.LastIndex(string(id), ".")
		if dot <= 0 {
			continue
		}
		parent, exists := issues[model.ID(string(id)[:dot])]
		if !exists {
			continue
		}
		switch {
		case parent.slug == child.slug:
			parents++
		case child.tombstone:
			census.Counts.OmittedParents++
		default:
			census.AmbiguousParents = append(census.AmbiguousParents, id)
		}
	}
	sort.Slice(census.AmbiguousParents, func(i, j int) bool {
		return census.AmbiguousParents[i] < census.AmbiguousParents[j]
	})

	census.Counts.IssueParents = parents
	// v7 keeps parentage in its own table, so the dependency count is what is
	// left once the parent-child edges move out of it.
	census.Counts.Dependencies = edges - len(explicit)
	return nil
}

const legacyParentDep = "parent-child"

func censusScalars(ctx context.Context, db *sql.DB, census *Census) error {
	for _, count := range []struct {
		query string
		into  *int
	}{
		{`SELECT count(*) FROM labels`, &census.Counts.Labels},
		{`SELECT count(*) FROM comments`, &census.Counts.Comments},
	} {
		if err := db.QueryRowContext(ctx, count.query).Scan(count.into); err != nil {
			return fmt.Errorf("census: %w", err)
		}
	}
	return nil
}

// censusMemories applies the same curation the plan will, and reports which
// Project namespaces the survivors need.
func censusMemories(ctx context.Context, db *sql.DB, slugOf map[int64]string, census *Census) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, project_id, kind, body, COALESCE(source, ''), superseded_by, deleted_at FROM memories`)
	if err != nil {
		return nil, fmt.Errorf("census memories: %w", err)
	}
	defer rows.Close()
	owners := map[string]bool{}
	for rows.Next() {
		var (
			legacy       store.LegacyMemory
			projectID    sql.NullInt64
			supersededBy sql.NullString
			deletedAt    sql.NullString
		)
		if err := rows.Scan(&legacy.ID, &projectID, &legacy.Kind, &legacy.Body,
			&legacy.Source, &supersededBy, &deletedAt); err != nil {
			return nil, fmt.Errorf("census memories: %w", err)
		}
		legacy.ProjectSlug = slugOf[projectID.Int64]
		legacy.Deleted = deletedAt.Valid
		if supersededBy.Valid {
			next := model.ID(supersededBy.String)
			legacy.SupersededBy = &next
		}
		curated, keep := Curate(legacy)
		if !keep {
			census.Counts.OmittedMemories++
			continue
		}
		census.Counts.Memories++
		owners[curated.ProjectSlug] = true
	}
	return owners, rows.Err()
}
