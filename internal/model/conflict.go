package model

import "fmt"

// RecordKind names every record shape carried by the mirror. Machine-local
// Workspace bindings and ID-owner storage rows are deliberately absent.
type RecordKind string

const (
	RecordProject           RecordKind = "project"
	RecordRepositoryLocator RecordKind = "repository_locator"
	RecordIssue             RecordKind = "issue"
	RecordIssueParent       RecordKind = "issue_parent"
	RecordDependency        RecordKind = "dependency"
	RecordLabel             RecordKind = "label"
	RecordComment           RecordKind = "comment"
	RecordMemory            RecordKind = "memory"
	RecordReplicaSuccession RecordKind = "replica_succession"
)

func ValidRecordKind(kind RecordKind) bool {
	switch kind {
	case RecordProject,
		RecordRepositoryLocator,
		RecordIssue,
		RecordIssueParent,
		RecordDependency,
		RecordLabel,
		RecordComment,
		RecordMemory,
		RecordReplicaSuccession:
		return true
	default:
		return false
	}
}
func supportsMergeConflict(kind RecordKind) bool {
	switch kind {
	case RecordProject,
		RecordRepositoryLocator,
		RecordIssue,
		RecordIssueParent,
		RecordDependency,
		RecordLabel,
		RecordMemory:
		return true
	default:
		return false
	}
}

func hasCreationOrigin(kind RecordKind) bool {
	switch kind {
	case RecordProject, RecordIssue, RecordComment, RecordMemory:
		return true
	default:
		return false
	}
}

// RecordRef is a printable mirror-record identity. Mirror owns the canonical
// encoding of composite keys; model only requires a known kind and nonempty key.
type RecordRef struct {
	Kind RecordKind `json:"kind"`
	Key  string     `json:"key"`
}

func (record RecordRef) Validate() error {
	if !ValidRecordKind(record.Kind) {
		return invalidValue("mirror record", "kind", string(record.Kind))
	}
	if record.Key == "" {
		return invalidValue("mirror record", "key", record.Key)
	}
	return nil
}

// MergeConflict records an equal-generation conflict resolved without failing
// the import. Chosen must follow delete-wins, then Replica-key ordering.
type MergeConflict struct {
	Record     RecordRef     `json:"record"`
	Current    RecordVersion `json:"current"`
	Incoming   RecordVersion `json:"incoming"`
	Chosen     RecordVersion `json:"chosen"`
	DeleteWins bool          `json:"delete_wins"`
}

func (conflict MergeConflict) Validate() error {
	if err := conflict.Record.Validate(); err != nil {
		return fieldError("merge conflict", "record", err)
	}
	if !supportsMergeConflict(conflict.Record.Kind) {
		return fmt.Errorf("%w: %s records cannot produce merge conflicts", ErrInvalid, conflict.Record.Kind)
	}
	if err := conflict.Current.Validate(); err != nil {
		return fieldError("merge conflict", "current version", err)
	}
	if err := conflict.Incoming.Validate(); err != nil {
		return fieldError("merge conflict", "incoming version", err)
	}
	if !conflict.Current.Revision.Concurrent(conflict.Incoming.Revision) {
		return fmt.Errorf("%w: merge conflict revisions are not concurrent", ErrInvalid)
	}

	chosen := conflict.Current
	deleteWins := conflict.Current.Tombstone != conflict.Incoming.Tombstone
	if deleteWins {
		if conflict.Incoming.Tombstone == Tombstoned {
			chosen = conflict.Incoming
		}
	} else if conflict.Incoming.Revision.Compare(conflict.Current.Revision) > 0 {
		chosen = conflict.Incoming
	}
	if conflict.Chosen != chosen {
		return fmt.Errorf("%w: merge conflict chosen version violates deterministic order", ErrInvalid)
	}
	if conflict.DeleteWins != deleteWins {
		return fmt.Errorf("%w: merge conflict delete-wins fact is inconsistent", ErrInvalid)
	}
	return nil
}

// IdentityCollision is a hard conflict: one opaque entity identity has two
// creation origins. Imports report both summaries and commit nothing.
type IdentityCollision struct {
	Record          RecordRef  `json:"record"`
	CurrentOrigin   ReplicaKey `json:"current_origin"`
	IncomingOrigin  ReplicaKey `json:"incoming_origin"`
	CurrentSummary  string     `json:"current_summary"`
	IncomingSummary string     `json:"incoming_summary"`
}

func (collision IdentityCollision) Validate() error {
	if err := collision.Record.Validate(); err != nil {
		return fieldError("identity collision", "record", err)
	}
	if !hasCreationOrigin(collision.Record.Kind) {
		return fmt.Errorf("%w: %s records have no creation origin", ErrInvalid, collision.Record.Kind)
	}
	if err := collision.CurrentOrigin.Validate(); err != nil {
		return fieldError("identity collision", "current origin", err)
	}
	if err := collision.IncomingOrigin.Validate(); err != nil {
		return fieldError("identity collision", "incoming origin", err)
	}
	if collision.CurrentOrigin == collision.IncomingOrigin {
		return fmt.Errorf("%w: identity collision origins must differ", ErrInvalid)
	}
	if collision.CurrentSummary == "" || collision.IncomingSummary == "" {
		return fmt.Errorf("%w: identity collision summaries must be nonempty", ErrInvalid)
	}
	return nil
}

func (collision IdentityCollision) Error() string {
	return fmt.Sprintf(
		"identity collision for %s %q: current origin %s (%s), incoming origin %s (%s)",
		collision.Record.Kind,
		collision.Record.Key,
		collision.CurrentOrigin,
		collision.CurrentSummary,
		collision.IncomingOrigin,
		collision.IncomingSummary,
	)
}

func (IdentityCollision) Unwrap() error { return ErrConflict }

// ContentConflict is a hard conflict: one exact revision identifies different
// canonical state in two snapshots.
type ContentConflict struct {
	Record          RecordRef `json:"record"`
	Revision        Revision  `json:"revision"`
	CurrentSummary  string    `json:"current_summary"`
	IncomingSummary string    `json:"incoming_summary"`
}

func (conflict ContentConflict) Validate() error {
	if err := conflict.Record.Validate(); err != nil {
		return fieldError("content conflict", "record", err)
	}
	if err := conflict.Revision.Validate(); err != nil {
		return fieldError("content conflict", "revision", err)
	}
	if conflict.CurrentSummary == "" || conflict.IncomingSummary == "" {
		return fmt.Errorf("%w: content conflict summaries must be nonempty", ErrInvalid)
	}
	if conflict.CurrentSummary == conflict.IncomingSummary {
		return fmt.Errorf("%w: content conflict summaries must differ", ErrInvalid)
	}
	return nil
}

func (conflict ContentConflict) Error() string {
	return fmt.Sprintf(
		"content conflict for %s %q at revision %d/%s: current %s, incoming %s",
		conflict.Record.Kind,
		conflict.Record.Key,
		conflict.Revision.Generation,
		conflict.Revision.Replica,
		conflict.CurrentSummary,
		conflict.IncomingSummary,
	)
}

func (ContentConflict) Unwrap() error { return ErrConflict }
