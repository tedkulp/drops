package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

const idAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"

type randomIDs struct{}

func (randomIDs) NewID(kind model.OwnerKind, parent *model.ID, reserved []model.ID) (model.ID, error) {
	if parent != nil {
		prefix := string(*parent) + "."
		maxSuffix := 0
		for _, id := range reserved {
			raw := string(id)
			if !strings.HasPrefix(raw, prefix) {
				continue
			}
			suffix, err := strconv.Atoi(strings.TrimPrefix(raw, prefix))
			if err == nil && suffix > maxSuffix {
				maxSuffix = suffix
			}
		}
		return model.ID(prefix + strconv.Itoa(maxSuffix+1)), nil
	}

	bytes := make([]byte, 5)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate ID: %w", err)
	}
	for i := range bytes {
		bytes[i] = idAlphabet[int(bytes[i])%len(idAlphabet)]
	}
	prefix := ""
	if kind == model.OwnerMemory {
		prefix = "mem-"
	}
	return model.ID(prefix + string(bytes)), nil
}

// CreateIssue is the caller-authored state for a new Issue. Core supplies
// identity, timestamps, revision, ownership, parentage, and label records.
type CreateIssue struct {
	Project       model.ProjectKey
	Parent        *model.ID
	Title         string
	Description   string
	Type          model.IssueType
	Priority      int
	Assignee      *string
	DeferredUntil *model.Timestamp
	Labels        []string
}

// CreateIssue atomically reserves the permanent ID and writes the Issue and its
// initial child records.
func (core *Core) CreateIssue(ctx context.Context, input CreateIssue) (model.Issue, error) {
	if input.Title == "" || !model.ValidIssueType(input.Type) || !model.ValidPriority(input.Priority) {
		return model.Issue{}, fmt.Errorf("%w: invalid Issue fields", model.ErrInvalid)
	}
	for _, label := range input.Labels {
		if label == "" {
			return model.Issue{}, fmt.Errorf("%w: empty label", model.ErrInvalid)
		}
	}

	project := input.Project
	if input.Parent != nil {
		parent, err := core.store.Issue(ctx, *input.Parent)
		if err != nil {
			return model.Issue{}, err
		}
		if project != "" && project != parent.ProjectKey {
			return model.Issue{}, fmt.Errorf("%w: child Project differs from parent Project", model.ErrInvalid)
		}
		project = parent.ProjectKey
	}
	owningProject, err := core.store.Project(ctx, project)
	if err != nil {
		return model.Issue{}, err
	}
	if owningProject.ArchivedAt != nil {
		return model.Issue{}, fmt.Errorf("%w: Project %s is archived", model.ErrInvalid, project)
	}

	for range 100 {
		id, err := core.nextID(ctx, model.OwnerIssue, input.Parent)
		if err != nil {
			return model.Issue{}, err
		}
		issue, err := core.putNewIssue(ctx, id, project, input)
		if errors.Is(err, model.ErrConflict) {
			continue
		}
		if err == nil {
			core.emitWarnings(model.RecordIssue, string(issue.ID), []textField{
				{"title", issue.Title},
				{"description", issue.Description},
				{"close_reason", stringValue(issue.CloseReason)},
			})
		}
		return issue, err
	}
	return model.Issue{}, fmt.Errorf("%w: could not reserve an Issue ID", model.ErrConflict)
}

func (core *Core) putNewIssue(ctx context.Context, id model.ID, project model.ProjectKey, input CreateIssue) (model.Issue, error) {
	revision, err := model.InitialRevision(core.replica)
	if err != nil {
		return model.Issue{}, err
	}
	stamp := core.timestamp()
	issue := model.Issue{
		ID:              id,
		ProjectKey:      project,
		Title:           input.Title,
		Description:     input.Description,
		Type:            input.Type,
		Status:          model.StatusOpen,
		Priority:        input.Priority,
		Assignee:        input.Assignee,
		DeferredUntil:   input.DeferredUntil,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
		CreationReplica: core.replica,
		Revision:        revision,
	}

	err = core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		if err := tx.PutIDOwner(ctx, model.IDOwner{ID: id, Kind: model.OwnerIssue, CreationReplica: core.replica}); err != nil {
			return err
		}
		if err := tx.PutIssue(ctx, issue); err != nil {
			return err
		}
		if input.Parent != nil {
			parent := model.IssueParent{ChildID: id, ParentID: *input.Parent, CreatedAt: stamp, Revision: revision}
			if err := tx.PutIssueParent(ctx, parent); err != nil {
				return err
			}
		}
		seen := make(map[string]struct{}, len(input.Labels))
		for _, name := range input.Labels {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			if err := tx.PutLabel(ctx, model.Label{IssueID: id, Name: name, Revision: revision}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return model.Issue{}, err
	}
	return issue, nil
}
