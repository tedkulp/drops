package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// IssueEdit changes ordinary Issue fields without changing lifecycle state.
type IssueEdit struct {
	Title         *string
	Description   *string
	Type          *model.IssueType
	Priority      *int
	Assignee      **string
	DeferredUntil **model.Timestamp
}

func (core *Core) EditIssue(ctx context.Context, id model.ID, edit IssueEdit) (model.Issue, error) {
	var changed model.Issue
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := tx.Issue(ctx, id)
		if err != nil {
			return err
		}
		if current.Tombstone == model.Tombstoned {
			return fmt.Errorf("%w: Issue %s is tombstoned", model.ErrConflict, id)
		}
		observed := current.Revision
		if edit.Title != nil {
			current.Title = *edit.Title
		}
		if edit.Description != nil {
			current.Description = *edit.Description
		}
		if edit.Type != nil {
			current.Type = *edit.Type
		}
		if edit.Priority != nil {
			current.Priority = *edit.Priority
		}
		if edit.Assignee != nil {
			current.Assignee = *edit.Assignee
		}
		if edit.DeferredUntil != nil {
			current.DeferredUntil = *edit.DeferredUntil
		}
		if err := current.Validate(); err != nil {
			return err
		}
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		current.UpdatedAt, current.Revision = core.timestamp(), next
		if err := tx.UpdateIssue(ctx, current, observed); err != nil {
			return err
		}
		changed = current
		return nil
	})
	if err == nil {
		core.emitWarnings(model.RecordIssue, string(id), []textField{{"title", changed.Title}, {"description", changed.Description}, {"close_reason", stringValue(changed.CloseReason)}})
	}
	return changed, err
}

// ClaimIssue assigns an unclaimed Issue without allowing one session to
// overwrite another session's claim.
func (core *Core) ClaimIssue(ctx context.Context, id model.ID, assignee string) (model.Issue, error) {
	if assignee == "" {
		return model.Issue{}, fmt.Errorf("%w: empty assignee", model.ErrInvalid)
	}
	var changed model.Issue
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := tx.Issue(ctx, id)
		if err != nil {
			return err
		}
		if current.Tombstone == model.Tombstoned {
			return fmt.Errorf("%w: Issue %s is tombstoned", model.ErrConflict, id)
		}
		if current.Assignee != nil && *current.Assignee != "" {
			return fmt.Errorf("%w: Issue %s is already claimed by %s", model.ErrConflict, id, *current.Assignee)
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		current.Assignee, current.UpdatedAt, current.Revision = &assignee, core.timestamp(), next
		if err := tx.UpdateIssue(ctx, current, observed); err != nil {
			return err
		}
		changed = current
		return nil
	})
	return changed, err
}

// ReleaseIssue removes an Issue's claim.
func (core *Core) ReleaseIssue(ctx context.Context, id model.ID) (model.Issue, error) {
	var unassigned *string
	return core.EditIssue(ctx, id, IssueEdit{Assignee: &unassigned})
}

func (core *Core) SetIssueTombstone(ctx context.Context, id model.ID, tombstone model.Tombstone) (model.Issue, error) {
	var changed model.Issue
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := tx.Issue(ctx, id)
		if err != nil {
			return err
		}
		if current.Tombstone == tombstone {
			changed = current
			return nil
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		current.Tombstone, current.UpdatedAt, current.Revision = tombstone, core.timestamp(), next
		if err := tx.UpdateIssue(ctx, current, observed); err != nil {
			return err
		}
		changed = current
		return nil
	})
	return changed, err
}

// MoveIssues changes every named Issue's Project in one transaction.
func (core *Core) MoveIssues(ctx context.Context, ids []model.ID, destination model.ProjectKey) ([]model.Issue, error) {
	moved := make([]model.Issue, 0, len(ids))
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		project, err := tx.Project(ctx, destination)
		if err != nil {
			return err
		}
		if project.ArchivedAt != nil {
			return fmt.Errorf("%w: destination Project is archived", model.ErrInvalid)
		}
		for _, id := range ids {
			issue, err := tx.Issue(ctx, id)
			if err != nil {
				return err
			}
			if issue.ProjectKey == destination {
				continue
			}
			observed := issue.Revision
			next, err := observed.Next(core.replica)
			if err != nil {
				return err
			}
			issue.ProjectKey, issue.UpdatedAt, issue.Revision = destination, core.timestamp(), next
			if err := tx.UpdateIssue(ctx, issue, observed); err != nil {
				return err
			}
			moved = append(moved, issue)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return moved, nil
}

// MemoryEdit changes notebook content or ownership without changing identity.
type MemoryEdit struct {
	Project    *model.ProjectKey
	Title      *string
	Body       *string
	Provenance **string
}

func (core *Core) EditMemory(ctx context.Context, id model.ID, edit MemoryEdit) (model.Memory, error) {
	var changed model.Memory
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := tx.Memory(ctx, id)
		if err != nil {
			return err
		}
		if edit.Project != nil && *edit.Project != current.ProjectKey {
			project, err := tx.Project(ctx, *edit.Project)
			if err != nil {
				return err
			}
			if project.ArchivedAt != nil {
				return fmt.Errorf("%w: destination Project is archived", model.ErrInvalid)
			}
			if err := refuseChainedMove(ctx, tx, current); err != nil {
				return err
			}
			current.ProjectKey = *edit.Project
		}
		if edit.Title != nil {
			current.Title = *edit.Title
		}
		if edit.Body != nil {
			current.Body = *edit.Body
		}
		if edit.Provenance != nil {
			current.Provenance = *edit.Provenance
		}
		if err := current.Validate(); err != nil {
			return err
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		current.UpdatedAt, current.Revision = core.timestamp(), next
		if err := tx.UpdateMemory(ctx, current, observed); err != nil {
			return err
		}
		changed = current
		return nil
	})
	if err == nil {
		core.emitWarnings(model.RecordMemory, string(id), []textField{{"title", changed.Title}, {"body", changed.Body}})
	}
	return changed, err
}

// refuseChainedMove rejects rescoping a Memory that stands in a supersession
// chain, in either direction.
//
// The schema pins a chain to one Project — memories carries FOREIGN KEY
// (superseded_by, project_key) REFERENCES memories(id, project_key) — so
// moving one link is a write SQLite refuses. Left to it, the agent gets
// "constraint failed: FOREIGN KEY constraint failed (787)" at exit 4, which
// names neither the rule nor the other memory. This states the rule instead,
// and states it as invalid input, which is what it is.
func refuseChainedMove(ctx context.Context, tx *store.Tx, memory model.Memory) error {
	if memory.SupersededBy != nil {
		return fmt.Errorf("%w: Memory %s was superseded by %s and a supersession chain lives in one Project; "+
			"edit %s instead, or forget this one", model.ErrInvalid, memory.ID, *memory.SupersededBy, *memory.SupersededBy)
	}
	siblings, err := tx.Memories(ctx, store.MemoryFilter{
		Project: &memory.ProjectKey, IncludeSuperseded: true, IncludeTombstoned: true,
	})
	if err != nil {
		return err
	}
	for _, sibling := range siblings {
		if sibling.SupersededBy != nil && *sibling.SupersededBy == memory.ID {
			return fmt.Errorf("%w: Memory %s supersedes %s and a supersession chain lives in one Project; "+
				"remember a fresh memory in the destination instead", model.ErrInvalid, memory.ID, sibling.ID)
		}
	}
	return nil
}

func (core *Core) SetMemoryTombstone(ctx context.Context, id model.ID, tombstone model.Tombstone) (model.Memory, error) {
	var changed model.Memory
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		current, err := tx.Memory(ctx, id)
		if err != nil {
			return err
		}
		if current.Tombstone == tombstone {
			changed = current
			return nil
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		current.Tombstone, current.UpdatedAt, current.Revision = tombstone, core.timestamp(), next
		if err := tx.UpdateMemory(ctx, current, observed); err != nil {
			return err
		}
		changed = current
		return nil
	})
	return changed, err
}

func (core *Core) AddComment(ctx context.Context, issueID model.ID, author, body string) (model.Comment, error) {
	if body == "" {
		return model.Comment{}, fmt.Errorf("%w: Comment body is empty", model.ErrInvalid)
	}
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return model.Comment{}, err
	}
	comment := model.Comment{
		ID: model.CommentID(string(issueID) + ":" + hex.EncodeToString(bytes)), IssueID: issueID,
		Author: author, Body: body, CreatedAt: core.timestamp(), CreationReplica: core.replica,
	}
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, err := tx.Issue(ctx, issueID); err != nil {
			return err
		}
		return tx.PutComment(ctx, comment)
	})
	if err == nil {
		core.emitWarnings(model.RecordComment, string(comment.ID), []textField{{"body", body}})
	}
	return comment, err
}

func (core *Core) DeleteComment(ctx context.Context, id model.CommentID) error {
	return core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error { return tx.DeleteComment(ctx, id) })
}
