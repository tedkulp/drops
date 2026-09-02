package core

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"

	memorytext "github.com/tedkulp/drops/internal/memory"
	"github.com/tedkulp/drops/internal/mirror"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// ImportReport accounts for routine replacements and every deterministic
// equal-generation resolution. Hard conflicts return an error and commit none.
type ImportReport struct {
	Applied   int                   `json:"applied"`
	Ignored   int                   `json:"ignored"`
	Conflicts []model.MergeConflict `json:"conflicts"`
}

type mergeAction uint8

const (
	mergeIgnore mergeAction = iota
	mergeInsert
	mergeUpdate
)

// Import validates and merges one complete snapshot in a single transaction.
func (core *Core) Import(ctx context.Context, snapshot mirror.Snapshot) (ImportReport, error) {
	return core.importSnapshot(ctx, snapshot, nil)
}

// ImportWithHead atomically merges a snapshot and records the accepted source
// head. Sync uses it so a crash cannot commit one fact without the other.
func (core *Core) ImportWithHead(ctx context.Context, snapshot mirror.Snapshot, head model.ReplicaHead) (ImportReport, error) {
	if err := head.Validate(); err != nil {
		return ImportReport{}, err
	}
	return core.importSnapshot(ctx, snapshot, &head)
}

func (core *Core) importSnapshot(ctx context.Context, snapshot mirror.Snapshot, head *model.ReplicaHead) (ImportReport, error) {
	records := append([]mirror.Record(nil), snapshot.Records...)
	seen := make(map[model.RecordRef]struct{}, len(records))
	for _, record := range records {
		if err := record.Validate(); err != nil {
			return ImportReport{}, err
		}
		ref, err := record.Ref()
		if err != nil {
			return ImportReport{}, err
		}
		if _, duplicate := seen[ref]; duplicate {
			return ImportReport{}, fmt.Errorf("%w: duplicate mirror record %s %q", model.ErrInvalid, ref.Kind, ref.Key)
		}
		seen[ref] = struct{}{}
		if project, ok := record.Value.(model.Project); ok {
			if err := validateReservedProject(project); err != nil {
				return ImportReport{}, err
			}
		}
	}
	slices.SortFunc(records, func(left, right mirror.Record) int {
		leftRank, rightRank := importRank(left.Kind), importRank(right.Kind)
		if leftRank != rightRank {
			return leftRank - rightRank
		}
		leftRef, _ := left.Ref()
		rightRef, _ := right.Ref()
		if leftRef.Key < rightRef.Key {
			return -1
		}
		if leftRef.Key > rightRef.Key {
			return 1
		}
		return 0
	})

	report := ImportReport{Conflicts: []model.MergeConflict{}}
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		for _, incoming := range records {
			current, found, err := currentRecord(ctx, tx, incoming)
			if err != nil {
				return err
			}
			if err := core.checkIdentity(ctx, tx, current, incoming, found); err != nil {
				return err
			}
			action, conflict, err := decideMerge(current, incoming, found)
			if err != nil {
				return err
			}
			if conflict != nil {
				report.Conflicts = append(report.Conflicts, *conflict)
			}
			switch action {
			case mergeIgnore:
				report.Ignored++
			case mergeInsert, mergeUpdate:
				if err := applyRecord(ctx, tx, incoming, action, current); err != nil {
					return err
				}
				report.Applied++
			}
		}
		memories, err := tx.Memories(ctx, store.MemoryFilter{IncludeSuperseded: true, IncludeTombstoned: true})
		if err != nil {
			return err
		}
		links := make([]memorytext.Link, len(memories))
		for i, entry := range memories {
			links[i] = memorytext.Link{ID: entry.ID, Project: entry.ProjectKey, SupersededBy: entry.SupersededBy}
		}
		if err := memorytext.ValidateSupersession(links); err != nil {
			return err
		}
		if err := validateImportedGraphs(ctx, tx); err != nil {
			return err
		}
		if head != nil {
			return tx.PutReplicaHead(ctx, *head)
		}
		return nil
	})
	if err != nil {
		return ImportReport{}, err
	}
	return report, nil
}

func validateReservedProject(project model.Project) error {
	reservedKey := isReservedProject(project.Key)
	reservedSlug := project.Slug == "global" || project.Slug == "inbox"
	if reservedKey {
		expected := "global"
		if project.Key == model.InboxProjectKey {
			expected = "inbox"
		}
		if project.Slug != expected || project.ArchivedAt != nil {
			return fmt.Errorf("%w: reserved Project %s must remain live with slug %q", model.ErrInvalid, project.Key, expected)
		}
	} else if reservedSlug {
		return fmt.Errorf("%w: reserved Project slug %q has the wrong key", model.ErrInvalid, project.Slug)
	}
	return nil
}

func validateImportedGraphs(ctx context.Context, tx *store.Tx) error {
	parents, err := tx.IssueParents(ctx)
	if err != nil {
		return err
	}
	liveParents := make(map[model.ID]model.ID, len(parents))
	for _, parent := range parents {
		if parent.Tombstone == model.Live {
			liveParents[parent.ChildID] = parent.ParentID
		}
	}
	for child := range liveParents {
		seen := map[model.ID]struct{}{child: {}}
		for ancestor, ok := liveParents[child]; ok; ancestor, ok = liveParents[ancestor] {
			if _, duplicate := seen[ancestor]; duplicate {
				return fmt.Errorf("%w: Issue parent cycle through %s", model.ErrInvalid, ancestor)
			}
			seen[ancestor] = struct{}{}
		}
	}

	successions, err := tx.ReplicaSuccessions(ctx)
	if err != nil {
		return err
	}
	next := make(map[model.ReplicaKey]model.ReplicaKey, len(successions))
	for _, succession := range successions {
		next[succession.OldReplica] = succession.NewReplica
	}
	for old := range next {
		seen := map[model.ReplicaKey]struct{}{old: {}}
		for replica, ok := next[old]; ok; replica, ok = next[replica] {
			if _, duplicate := seen[replica]; duplicate {
				return fmt.Errorf("%w: Replica succession cycle through %s", model.ErrInvalid, replica)
			}
			seen[replica] = struct{}{}
		}
	}
	return nil
}

func importRank(kind model.RecordKind) int {
	switch kind {
	case model.RecordProject:
		return 0
	case model.RecordIssue, model.RecordMemory:
		return 1
	case model.RecordRepositoryLocator, model.RecordIssueParent, model.RecordDependency, model.RecordLabel, model.RecordComment:
		return 2
	case model.RecordReplicaSuccession:
		return 3
	default:
		return 4
	}
}

func currentRecord(ctx context.Context, tx *store.Tx, incoming mirror.Record) (mirror.Record, bool, error) {
	var value any
	var err error
	switch record := incoming.Value.(type) {
	case model.Project:
		value, err = tx.Project(ctx, record.Key)
	case model.RepositoryLocator:
		value, err = tx.RepositoryLocator(ctx, record.ProjectKey, record.Locator)
	case model.Issue:
		value, err = tx.Issue(ctx, record.ID)
	case model.IssueParent:
		value, err = tx.IssueParent(ctx, record.ChildID)
	case model.Dependency:
		value, err = tx.Dependency(ctx, record.FromID, record.ToID, record.Type)
	case model.Label:
		value, err = tx.Label(ctx, record.IssueID, record.Name)
	case model.Comment:
		value, err = tx.Comment(ctx, record.ID)
	case model.Memory:
		value, err = tx.Memory(ctx, record.ID)
	case model.ReplicaSuccession:
		var successions []model.ReplicaSuccession
		successions, err = tx.ReplicaSuccessions(ctx)
		if err == nil {
			err = model.ErrNotFound
			for _, existing := range successions {
				if existing.OldReplica == record.OldReplica && existing.NewReplica == record.NewReplica {
					value, err = existing, nil
					break
				}
			}
		}
	default:
		return mirror.Record{}, false, fmt.Errorf("%w: unsupported record %T", model.ErrInvalid, incoming.Value)
	}
	if errors.Is(err, model.ErrNotFound) {
		return mirror.Record{}, false, nil
	}
	if err != nil {
		return mirror.Record{}, false, err
	}
	wrapped, err := mirror.NewRecord(value)
	return wrapped, true, err
}

func (core *Core) checkIdentity(ctx context.Context, tx *store.Tx, current, incoming mirror.Record, found bool) error {
	ref, _ := incoming.Ref()
	switch value := incoming.Value.(type) {
	case model.Issue:
		return checkOwnedIdentity(ctx, tx, ref, value.ID, model.OwnerIssue, value.CreationReplica, incoming)
	case model.Memory:
		return checkOwnedIdentity(ctx, tx, ref, value.ID, model.OwnerMemory, value.CreationReplica, incoming)
	case model.Project:
		if found && current.Value.(model.Project).CreationReplica != value.CreationReplica {
			return identityCollision(ref, current, incoming, current.Value.(model.Project).CreationReplica, value.CreationReplica)
		}
	case model.Comment:
		if found && current.Value.(model.Comment).CreationReplica != value.CreationReplica {
			return identityCollision(ref, current, incoming, current.Value.(model.Comment).CreationReplica, value.CreationReplica)
		}
	}
	return nil
}

func checkOwnedIdentity(ctx context.Context, tx *store.Tx, ref model.RecordRef, id model.ID, kind model.OwnerKind, origin model.ReplicaKey, incoming mirror.Record) error {
	owner, err := tx.IDOwner(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if owner.Kind == kind && owner.CreationReplica == origin {
		return nil
	}
	currentSummary := fmt.Sprintf("reserved as %s from %s", owner.Kind, owner.CreationReplica)
	return model.IdentityCollision{
		Record: ref, CurrentOrigin: owner.CreationReplica, IncomingOrigin: origin,
		CurrentSummary: currentSummary, IncomingSummary: recordSummary(incoming),
	}
}

func identityCollision(ref model.RecordRef, current, incoming mirror.Record, currentOrigin, incomingOrigin model.ReplicaKey) error {
	return model.IdentityCollision{
		Record: ref, CurrentOrigin: currentOrigin, IncomingOrigin: incomingOrigin,
		CurrentSummary: recordSummary(current), IncomingSummary: recordSummary(incoming),
	}
}

func decideMerge(current, incoming mirror.Record, found bool) (mergeAction, *model.MergeConflict, error) {
	if !found {
		return mergeInsert, nil, nil
	}
	if incoming.Kind == model.RecordComment || incoming.Kind == model.RecordReplicaSuccession {
		if reflect.DeepEqual(current.Value, incoming.Value) {
			return mergeIgnore, nil, nil
		}
		ref, _ := incoming.Ref()
		return mergeIgnore, nil, model.ContentConflict{Record: ref, Revision: recordVersion(incoming).Revision, CurrentSummary: recordSummary(current), IncomingSummary: recordSummary(incoming)}
	}
	currentVersion, incomingVersion := recordVersion(current), recordVersion(incoming)
	if incomingVersion.Revision.Generation < currentVersion.Revision.Generation {
		return mergeIgnore, nil, nil
	}
	if incomingVersion.Revision.Generation > currentVersion.Revision.Generation {
		return mergeUpdate, nil, nil
	}
	if incomingVersion.Revision.Replica == currentVersion.Revision.Replica {
		if reflect.DeepEqual(current.Value, incoming.Value) {
			return mergeIgnore, nil, nil
		}
		ref, _ := incoming.Ref()
		return mergeIgnore, nil, model.ContentConflict{Record: ref, Revision: incomingVersion.Revision, CurrentSummary: recordSummary(current), IncomingSummary: recordSummary(incoming)}
	}

	chosen := currentVersion
	deleteWins := currentVersion.Tombstone != incomingVersion.Tombstone
	if deleteWins {
		if incomingVersion.Tombstone == model.Tombstoned {
			chosen = incomingVersion
		}
	} else if incomingVersion.Revision.Compare(currentVersion.Revision) > 0 {
		chosen = incomingVersion
	}
	ref, _ := incoming.Ref()
	conflict := &model.MergeConflict{Record: ref, Current: currentVersion, Incoming: incomingVersion, Chosen: chosen, DeleteWins: deleteWins}
	if chosen == incomingVersion {
		return mergeUpdate, conflict, nil
	}
	return mergeIgnore, conflict, nil
}

func recordVersion(record mirror.Record) model.RecordVersion {
	switch value := record.Value.(type) {
	case model.Project:
		return model.RecordVersion{Revision: value.Revision}
	case model.RepositoryLocator:
		return model.RecordVersion{Revision: value.Revision, Tombstone: value.Tombstone}
	case model.Issue:
		return model.RecordVersion{Revision: value.Revision, Tombstone: value.Tombstone}
	case model.IssueParent:
		return model.RecordVersion{Revision: value.Revision, Tombstone: value.Tombstone}
	case model.Dependency:
		return model.RecordVersion{Revision: value.Revision, Tombstone: value.Tombstone}
	case model.Label:
		return model.RecordVersion{Revision: value.Revision, Tombstone: value.Tombstone}
	case model.Comment:
		return model.RecordVersion{Revision: value.Revision()}
	case model.Memory:
		return model.RecordVersion{Revision: value.Revision, Tombstone: value.Tombstone}
	case model.ReplicaSuccession:
		return model.RecordVersion{Revision: model.Revision{Generation: 1, Replica: value.OldReplica}}
	default:
		panic("core: recordVersion for unsupported value")
	}
}

func applyRecord(ctx context.Context, tx *store.Tx, incoming mirror.Record, action mergeAction, current mirror.Record) error {
	switch value := incoming.Value.(type) {
	case model.Project:
		if action == mergeInsert {
			return tx.PutProject(ctx, value)
		}
		return tx.UpdateProject(ctx, value, current.Value.(model.Project).Revision)
	case model.RepositoryLocator:
		if action == mergeInsert {
			return tx.PutRepositoryLocator(ctx, value)
		}
		return tx.UpdateRepositoryLocator(ctx, value, current.Value.(model.RepositoryLocator).Revision)
	case model.Issue:
		if action == mergeInsert {
			if err := tx.PutIDOwner(ctx, model.IDOwner{ID: value.ID, Kind: model.OwnerIssue, CreationReplica: value.CreationReplica}); err != nil {
				return err
			}
			return tx.PutIssue(ctx, value)
		}
		return tx.UpdateIssue(ctx, value, current.Value.(model.Issue).Revision)
	case model.IssueParent:
		if action == mergeInsert {
			return tx.PutIssueParent(ctx, value)
		}
		return tx.UpdateIssueParent(ctx, value, current.Value.(model.IssueParent).Revision)
	case model.Dependency:
		if action == mergeInsert {
			return tx.PutDependency(ctx, value)
		}
		return tx.UpdateDependency(ctx, value, current.Value.(model.Dependency).Revision)
	case model.Label:
		if action == mergeInsert {
			return tx.PutLabel(ctx, value)
		}
		return tx.UpdateLabel(ctx, value, current.Value.(model.Label).Revision)
	case model.Comment:
		return tx.PutComment(ctx, value)
	case model.Memory:
		if action == mergeInsert {
			if err := tx.PutIDOwner(ctx, model.IDOwner{ID: value.ID, Kind: model.OwnerMemory, CreationReplica: value.CreationReplica}); err != nil {
				return err
			}
			return tx.PutMemory(ctx, value)
		}
		return tx.UpdateMemory(ctx, value, current.Value.(model.Memory).Revision)
	case model.ReplicaSuccession:
		return tx.PutReplicaSuccession(ctx, value)
	default:
		return fmt.Errorf("%w: unsupported record %T", model.ErrInvalid, incoming.Value)
	}
}

func recordSummary(record mirror.Record) string {
	encoded, err := mirror.Marshal(mirror.Snapshot{Records: []mirror.Record{record}})
	if err != nil {
		return fmt.Sprintf("invalid:%T", record.Value)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest[:8])
}
