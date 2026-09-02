package store

import (
	"context"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

const commentColumns = `id, issue_id, author, body, created_at, creation_replica`

func scanComment(row scanner) (model.Comment, error) {
	var comment model.Comment
	err := row.Scan(
		&comment.ID,
		&comment.IssueID,
		&comment.Author,
		&comment.Body,
		&comment.CreatedAt,
		&comment.CreationReplica,
	)
	return comment, err
}

// Comment reads one Comment.
func (read reader) Comment(ctx context.Context, id model.CommentID) (model.Comment, error) {
	row := read.ex.QueryRowContext(ctx,
		`SELECT `+commentColumns+` FROM comments WHERE id = ?`, string(id))
	comment, err := scanComment(row)
	if err != nil {
		return model.Comment{}, wrapNotFound(fmt.Sprintf("Comment %s", id), err)
	}
	return comment, nil
}

// IssueComments lists one Issue's thread oldest first, which is the order it is
// read in. Comments carry no tombstone: a thread is append-only.
func (read reader) IssueComments(ctx context.Context, issue model.ID) ([]model.Comment, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+commentColumns+` FROM comments
		 WHERE issue_id = ? ORDER BY created_at, id`, string(issue))
	return collect(rows, err, "list Issue comments", scanComment)
}

// Comments lists every Comment for export.
func (read reader) Comments(ctx context.Context) ([]model.Comment, error) {
	rows, err := read.ex.QueryContext(ctx,
		`SELECT `+commentColumns+` FROM comments ORDER BY id`)
	return collect(rows, err, "list comments", scanComment)
}

// PutComment appends a Comment. There is no update: a Comment is immutable, so
// its mirror revision is always generation one under its creation replica and is
// derived rather than stored. Removing one is a delete of the row, which is why
// DeleteComment exists where no other record has one.
func (tx *Tx) PutComment(ctx context.Context, comment model.Comment) error {
	if err := comment.Validate(); err != nil {
		return err
	}
	const statement = `INSERT INTO comments (` + commentColumns + `) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.ex.ExecContext(ctx, statement,
		string(comment.ID),
		string(comment.IssueID),
		comment.Author,
		comment.Body,
		string(comment.CreatedAt),
		string(comment.CreationReplica),
	)
	if err != nil {
		return classify(fmt.Sprintf("insert Comment %s", comment.ID), err)
	}
	return tx.bumpWriteSeq(ctx)
}

// DeleteComment removes a Comment outright. A Comment has no tombstone to set,
// so an obvious mistake is retracted by deleting the row; the mirror stops
// carrying it rather than carrying a removal.
func (tx *Tx) DeleteComment(ctx context.Context, id model.CommentID) error {
	result, err := tx.ex.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, string(id))
	if err != nil {
		return classify(fmt.Sprintf("delete Comment %s", id), err)
	}
	changed, err := affected(result, "delete Comment")
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("%w: Comment %s", model.ErrNotFound, id)
	}
	return tx.bumpWriteSeq(ctx)
}
