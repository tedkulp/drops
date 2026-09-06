package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// SetIssueStatus applies lifecycle timestamps and close metadata as one Issue
// record update.
func (core *Core) SetIssueStatus(ctx context.Context, id model.ID, status model.Status, reason string) (model.Issue, error) {
	if !model.ValidStatus(status) {
		return model.Issue{}, fmt.Errorf("%w: invalid Issue status %q", model.ErrInvalid, status)
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
		if current.Status == status && (status != model.StatusClosed || stringValue(current.CloseReason) == reason) {
			changed = current
			return nil
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		stamp := core.timestamp()
		current.Status = status
		current.UpdatedAt = stamp
		current.Revision = next
		switch status {
		case model.StatusOpen:
			current.ClosedAt = nil
			current.CloseReason = nil
		case model.StatusInProgress:
			if current.StartedAt == nil {
				current.StartedAt = &stamp
			}
			current.ClosedAt = nil
			current.CloseReason = nil
		case model.StatusClosed:
			current.ClosedAt = &stamp
			if reason == "" {
				current.CloseReason = nil
			} else {
				current.CloseReason = &reason
			}
		}
		if err := tx.UpdateIssue(ctx, current, observed); err != nil {
			return err
		}
		changed = current
		return nil
	})
	if err == nil {
		core.emitWarnings(model.RecordIssue, string(id), []textField{{"close_reason", stringValue(changed.CloseReason)}})
	}
	return changed, err
}

// SetLabel makes one independently revisioned membership live or tombstoned.
func (core *Core) SetLabel(ctx context.Context, issueID model.ID, name string, present bool) (model.Label, error) {
	if name == "" {
		return model.Label{}, fmt.Errorf("%w: empty label", model.ErrInvalid)
	}
	var changed model.Label
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, err := tx.Issue(ctx, issueID); err != nil {
			return err
		}
		current, err := tx.Label(ctx, issueID, name)
		if errors.Is(err, model.ErrNotFound) {
			if !present {
				return err
			}
			revision, revisionErr := model.InitialRevision(core.replica)
			if revisionErr != nil {
				return revisionErr
			}
			changed = model.Label{IssueID: issueID, Name: name, Revision: revision}
			return tx.PutLabel(ctx, changed)
		}
		if err != nil {
			return err
		}
		desired := model.Tombstoned
		if present {
			desired = model.Live
		}
		if current.Tombstone == desired {
			changed = current
			return nil
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		current.Tombstone, current.Revision = desired, next
		changed = current
		return tx.UpdateLabel(ctx, current, observed)
	})
	return changed, err
}

// SetDependency makes one typed edge live or tombstoned.
//
// An UNDIRECTED type is one relation however it is spelled, so the reverse row
// is the same edge and is acted on with it: adding the reciprocal `related`
// edge is the no-op that adding it twice already was, and removing one
// withdraws whichever direction the store holds — both of them, when sync has
// merged one from each replica. Before y7f6z the reciprocal spelling stored a
// second row, which a reader then listed twice.
func (core *Core) SetDependency(ctx context.Context, from, to model.ID, kind model.DependencyType, present bool) (model.Dependency, error) {
	if !model.ValidDependencyType(kind) || from == to {
		return model.Dependency{}, fmt.Errorf("%w: invalid dependency", model.ErrInvalid)
	}
	var changed model.Dependency
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, err := tx.Issue(ctx, from); err != nil {
			return err
		}
		if _, err := tx.Issue(ctx, to); err != nil {
			return err
		}
		var reverse *model.Dependency
		if kind.Undirected() {
			stored, err := tx.Dependency(ctx, to, from, kind)
			switch {
			case err == nil:
				reverse = &stored
			case !errors.Is(err, model.ErrNotFound):
				return err
			}
		}
		current, err := tx.Dependency(ctx, from, to, kind)
		if errors.Is(err, model.ErrNotFound) {
			// The reverse spelling of an undirected edge IS this edge, so it
			// is what gets revived or withdrawn rather than a second row.
			if reverse != nil {
				changed, err = core.setDependencyPresence(ctx, tx, *reverse, present)
				return err
			}
			if !present {
				return err
			}
			revision, revisionErr := model.InitialRevision(core.replica)
			if revisionErr != nil {
				return revisionErr
			}
			changed = model.Dependency{FromID: from, ToID: to, Type: kind, CreatedAt: core.timestamp(), Revision: revision}
			return tx.PutDependency(ctx, changed)
		}
		if err != nil {
			return err
		}
		// A live reverse row IS this relation, so reviving a tombstoned
		// forward row beside it would make a second live row for one
		// relation. That state is sync's too: one replica removed the edge
		// while the other added the reciprocal, and neither record knows
		// about the other.
		if present && current.Tombstone == model.Tombstoned && reverse != nil && reverse.Tombstone == model.Live {
			changed = *reverse
			return nil
		}
		changed, err = core.setDependencyPresence(ctx, tx, current, present)
		if err != nil {
			return err
		}
		// A reciprocal pair only sync could have written is still one relation
		// to every reader, so withdrawing it withdraws both halves. Adding
		// touches the spelling asked for and leaves the other alone: the
		// reader merges them either way.
		if !present && reverse != nil {
			if _, err := core.setDependencyPresence(ctx, tx, *reverse, present); err != nil {
				return err
			}
		}
		return nil
	})
	return changed, err
}

// setDependencyPresence moves one stored edge to the wanted liveness under
// compare-and-swap, leaving an edge already there untouched — so a repeated
// add advances no revision.
func (core *Core) setDependencyPresence(ctx context.Context, tx *store.Tx, current model.Dependency, present bool) (model.Dependency, error) {
	desired := model.Tombstoned
	if present {
		desired = model.Live
	}
	if current.Tombstone == desired {
		return current, nil
	}
	observed := current.Revision
	next, err := observed.Next(core.replica)
	if err != nil {
		return model.Dependency{}, err
	}
	current.Tombstone, current.Revision = desired, next
	if err := tx.UpdateDependency(ctx, current, observed); err != nil {
		return model.Dependency{}, err
	}
	return current, nil
}

// SetIssueParent sets, moves, restores, or tombstones the sole parent record.
func (core *Core) SetIssueParent(ctx context.Context, child model.ID, parent *model.ID) (model.IssueParent, error) {
	var changed model.IssueParent
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if _, err := tx.Issue(ctx, child); err != nil {
			return err
		}
		if parent != nil {
			if *parent == child {
				return fmt.Errorf("%w: an Issue cannot parent itself", model.ErrInvalid)
			}
			if _, err := tx.Issue(ctx, *parent); err != nil {
				return err
			}
			for ancestor := *parent; ; {
				relation, err := tx.IssueParent(ctx, ancestor)
				if errors.Is(err, model.ErrNotFound) || relation.Tombstone == model.Tombstoned {
					break
				}
				if err != nil {
					return err
				}
				if relation.ParentID == child {
					return fmt.Errorf("%w: Issue parent cycle", model.ErrInvalid)
				}
				ancestor = relation.ParentID
			}
		}
		current, err := tx.IssueParent(ctx, child)
		if errors.Is(err, model.ErrNotFound) {
			if parent == nil {
				return err
			}
			revision, revisionErr := model.InitialRevision(core.replica)
			if revisionErr != nil {
				return revisionErr
			}
			changed = model.IssueParent{ChildID: child, ParentID: *parent, CreatedAt: core.timestamp(), Revision: revision}
			return tx.PutIssueParent(ctx, changed)
		}
		if err != nil {
			return err
		}
		desired := model.Tombstoned
		if parent != nil {
			desired = model.Live
		}
		if current.Tombstone == desired && (parent == nil || current.ParentID == *parent) {
			changed = current
			return nil
		}
		observed := current.Revision
		next, err := observed.Next(core.replica)
		if err != nil {
			return err
		}
		if parent != nil {
			current.ParentID = *parent
		}
		current.Tombstone, current.Revision = desired, next
		changed = current
		return tx.UpdateIssueParent(ctx, current, observed)
	})
	return changed, err
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
