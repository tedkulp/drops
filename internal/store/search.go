package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/tedkulp/drops/internal/model"
)

// SearchIssues lists the Issues whose title, description or thread matches. A
// match in a Comment is deliberately indistinguishable from a match in a
// description: the answer is one row per Issue either way, in the same queue
// order as an ordinary listing, so relevance never reorders a queue a reader is
// working through.
func (read reader) SearchIssues(ctx context.Context, query string, filter IssueFilter) ([]model.Issue, error) {
	match, err := ftsQuery(query)
	if err != nil {
		return nil, err
	}
	const predicate = `(issues.rowid IN (SELECT rowid FROM issues_fts WHERE issues_fts MATCH ?)
	                    OR issues.id IN (SELECT issue_id FROM comments
	                                     WHERE comments.rowid IN (SELECT rowid FROM comments_fts
	                                                              WHERE comments_fts MATCH ?)))`
	return read.issuesWhere(ctx, filter, predicate, match, match)
}

// SearchMemories lists the Memories whose title or body matches.
func (read reader) SearchMemories(ctx context.Context, query string, filter MemoryFilter) ([]model.Memory, error) {
	match, err := ftsQuery(query)
	if err != nil {
		return nil, err
	}
	predicates, args := filter.predicates()
	predicates = append(predicates,
		`memories.rowid IN (SELECT rowid FROM memories_fts WHERE memories_fts MATCH ?)`)
	args = append(args, match)

	statement := memorySelect + " WHERE " + strings.Join(predicates, " AND ") +
		" ORDER BY memories.updated_at DESC, memories.id"
	if filter.Limit > 0 {
		statement += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := read.ex.QueryContext(ctx, statement, args...)
	return collect(rows, err, "search Memories", scanMemory)
}

// ftsQuery turns a user's search text into an FTS5 expression.
//
// Every term is quoted, so the FTS5 operator vocabulary — NEAR, OR, ^, *, the
// column filter ':' — is treated as text rather than syntax. A user searching
// for "NOT" is searching for the word, and an unbalanced quote is a search, not
// a syntax error the CLI would have to explain.
func ftsQuery(raw string) (string, error) {
	terms := strings.Fields(raw)
	if len(terms) == 0 {
		return "", fmt.Errorf("%w: search needs a term", model.ErrInvalid)
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " AND "), nil
}
