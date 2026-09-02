package model

import "fmt"

// CommentID is an opaque Comment identity. It is not part of the shared
// Issue/Memory ID namespace.
type CommentID string

// IDOwner is the permanent reservation of one Issue or Memory ID.
type IDOwner struct {
	ID              ID         `json:"id"`
	Kind            OwnerKind  `json:"kind"`
	CreationReplica ReplicaKey `json:"creation_replica"`
}

func (owner IDOwner) Validate() error {
	if err := owner.ID.Validate(); err != nil {
		return fieldError("ID owner", "id", err)
	}
	if !ValidOwnerKind(owner.Kind) {
		return invalidValue("ID owner", "kind", string(owner.Kind))
	}
	if err := owner.CreationReplica.Validate(); err != nil {
		return fieldError("ID owner", "creation replica", err)
	}
	return nil
}

// Project is one atomic replicated Project record. Repository locators are
// separate records and Workspace bindings are local records.
type Project struct {
	Key             ProjectKey `json:"project_key"`
	Slug            string     `json:"slug"`
	ArchivedAt      *Timestamp `json:"archived_at,omitempty"`
	CreatedAt       Timestamp  `json:"created_at"`
	UpdatedAt       Timestamp  `json:"updated_at"`
	CreationReplica ReplicaKey `json:"creation_replica"`
	Revision        Revision   `json:"revision"`
}

func (project Project) Validate() error {
	if err := project.Key.Validate(); err != nil {
		return fieldError("Project", "key", err)
	}
	if project.Slug == "" {
		return invalidValue("Project", "slug", project.Slug)
	}
	if err := validateOptionalTimestamp(project.ArchivedAt); err != nil {
		return fieldError("Project", "archived_at", err)
	}
	if err := project.CreatedAt.Validate(); err != nil {
		return fieldError("Project", "created_at", err)
	}
	if err := project.UpdatedAt.Validate(); err != nil {
		return fieldError("Project", "updated_at", err)
	}
	if err := project.CreationReplica.Validate(); err != nil {
		return fieldError("Project", "creation replica", err)
	}
	if err := project.Revision.Validate(); err != nil {
		return fieldError("Project", "revision", err)
	}
	return nil
}

// RepositoryLocator is synced discovery evidence for a Project.
type RepositoryLocator struct {
	ProjectKey ProjectKey `json:"project_key"`
	Locator    string     `json:"locator"`
	Tombstone  Tombstone  `json:"tombstoned"`
	Revision   Revision   `json:"revision"`
}

func (locator RepositoryLocator) Validate() error {
	if err := locator.ProjectKey.Validate(); err != nil {
		return fieldError("repository locator", "project key", err)
	}
	if locator.Locator == "" {
		return invalidValue("repository locator", "locator", locator.Locator)
	}
	if err := locator.Revision.Validate(); err != nil {
		return fieldError("repository locator", "revision", err)
	}
	return nil
}

// WorkspaceBinding is a machine-local path-to-Project association.
type WorkspaceBinding struct {
	Path       string     `json:"path"`
	ProjectKey ProjectKey `json:"project_key"`
}

func (binding WorkspaceBinding) Validate() error {
	if binding.Path == "" {
		return invalidValue("workspace binding", "path", binding.Path)
	}
	if err := binding.ProjectKey.Validate(); err != nil {
		return fieldError("workspace binding", "project key", err)
	}
	return nil
}

// Issue is one atomic replicated Issue record. Labels, dependencies, parentage,
// and comments are separate records.
type Issue struct {
	ID              ID         `json:"id"`
	ProjectKey      ProjectKey `json:"project_key"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Type            IssueType  `json:"issue_type"`
	Status          Status     `json:"status"`
	Priority        int        `json:"priority"`
	Assignee        *string    `json:"assignee,omitempty"`
	CloseReason     *string    `json:"close_reason,omitempty"`
	DeferredUntil   *Timestamp `json:"deferred_until,omitempty"`
	CreatedAt       Timestamp  `json:"created_at"`
	UpdatedAt       Timestamp  `json:"updated_at"`
	ClosedAt        *Timestamp `json:"closed_at,omitempty"`
	StartedAt       *Timestamp `json:"started_at,omitempty"`
	Tombstone       Tombstone  `json:"tombstoned"`
	CreationReplica ReplicaKey `json:"creation_replica"`
	Revision        Revision   `json:"revision"`
}

func (issue Issue) Validate() error {
	if err := issue.ID.Validate(); err != nil {
		return fieldError("Issue", "id", err)
	}
	if err := issue.ProjectKey.Validate(); err != nil {
		return fieldError("Issue", "project key", err)
	}
	if issue.Title == "" {
		return invalidValue("Issue", "title", issue.Title)
	}
	if !ValidIssueType(issue.Type) {
		return invalidValue("Issue", "type", string(issue.Type))
	}
	if !ValidStatus(issue.Status) {
		return invalidValue("Issue", "status", string(issue.Status))
	}
	if !ValidPriority(issue.Priority) {
		return fmt.Errorf("%w: Issue priority %d is outside 0..4", ErrInvalid, issue.Priority)
	}
	if err := validateOptionalTimestamp(issue.DeferredUntil); err != nil {
		return fieldError("Issue", "deferred_until", err)
	}
	if err := validateOptionalTimestamp(issue.ClosedAt); err != nil {
		return fieldError("Issue", "closed_at", err)
	}
	if err := validateOptionalTimestamp(issue.StartedAt); err != nil {
		return fieldError("Issue", "started_at", err)
	}
	if err := issue.CreatedAt.Validate(); err != nil {
		return fieldError("Issue", "created_at", err)
	}
	if err := issue.UpdatedAt.Validate(); err != nil {
		return fieldError("Issue", "updated_at", err)
	}
	if err := issue.CreationReplica.Validate(); err != nil {
		return fieldError("Issue", "creation replica", err)
	}
	if err := issue.Revision.Validate(); err != nil {
		return fieldError("Issue", "revision", err)
	}
	return nil
}

// IssueParent is the sole parentage authority. ID spelling has no semantic
// effect after legacy migration.
type IssueParent struct {
	ChildID   ID        `json:"child_id"`
	ParentID  ID        `json:"parent_id"`
	CreatedAt Timestamp `json:"created_at"`
	Tombstone Tombstone `json:"tombstoned"`
	Revision  Revision  `json:"revision"`
}

func (parent IssueParent) Validate() error {
	if err := parent.ChildID.Validate(); err != nil {
		return fieldError("Issue parent", "child id", err)
	}
	if err := parent.ParentID.Validate(); err != nil {
		return fieldError("Issue parent", "parent id", err)
	}
	if parent.ChildID == parent.ParentID {
		return fmt.Errorf("%w: an Issue cannot parent itself", ErrInvalid)
	}
	if err := parent.CreatedAt.Validate(); err != nil {
		return fieldError("Issue parent", "created_at", err)
	}
	if err := parent.Revision.Validate(); err != nil {
		return fieldError("Issue parent", "revision", err)
	}
	return nil
}

// Dependency is one revisioned generic relationship between two Issues.
type Dependency struct {
	FromID    ID             `json:"from_id"`
	ToID      ID             `json:"to_id"`
	Type      DependencyType `json:"dep_type"`
	CreatedAt Timestamp      `json:"created_at"`
	Tombstone Tombstone      `json:"tombstoned"`
	Revision  Revision       `json:"revision"`
}

func (dependency Dependency) Validate() error {
	if err := dependency.FromID.Validate(); err != nil {
		return fieldError("dependency", "from id", err)
	}
	if err := dependency.ToID.Validate(); err != nil {
		return fieldError("dependency", "to id", err)
	}
	if dependency.FromID == dependency.ToID {
		return fmt.Errorf("%w: an Issue cannot depend on itself", ErrInvalid)
	}
	if !ValidDependencyType(dependency.Type) {
		return invalidValue("dependency", "type", string(dependency.Type))
	}
	if err := dependency.CreatedAt.Validate(); err != nil {
		return fieldError("dependency", "created_at", err)
	}
	if err := dependency.Revision.Validate(); err != nil {
		return fieldError("dependency", "revision", err)
	}
	return nil
}

// Label is one independently revisioned Issue-label membership.
type Label struct {
	IssueID   ID        `json:"issue_id"`
	Name      string    `json:"label"`
	Tombstone Tombstone `json:"tombstoned"`
	Revision  Revision  `json:"revision"`
}

func (label Label) Validate() error {
	if err := label.IssueID.Validate(); err != nil {
		return fieldError("label", "Issue id", err)
	}
	if label.Name == "" {
		return invalidValue("label", "name", label.Name)
	}
	if err := label.Revision.Validate(); err != nil {
		return fieldError("label", "revision", err)
	}
	return nil
}

// Comment is immutable. Its mirror revision is always generation one under
// CreationReplica and therefore is not stored separately.
type Comment struct {
	ID              CommentID  `json:"id"`
	IssueID         ID         `json:"issue_id"`
	Author          string     `json:"author"`
	Body            string     `json:"body"`
	CreatedAt       Timestamp  `json:"created_at"`
	CreationReplica ReplicaKey `json:"creation_replica"`
}

func (comment Comment) Validate() error {
	if comment.ID == "" {
		return invalidValue("Comment", "id", string(comment.ID))
	}
	if err := comment.IssueID.Validate(); err != nil {
		return fieldError("Comment", "Issue id", err)
	}
	if err := comment.CreatedAt.Validate(); err != nil {
		return fieldError("Comment", "created_at", err)
	}
	if err := comment.CreationReplica.Validate(); err != nil {
		return fieldError("Comment", "creation replica", err)
	}
	return nil
}

func (comment Comment) Revision() Revision {
	return Revision{Generation: 1, Replica: comment.CreationReplica}
}

// Memory is one atomic replicated notebook record.
type Memory struct {
	ID              ID         `json:"id"`
	ProjectKey      ProjectKey `json:"project_key"`
	Title           string     `json:"title"`
	Body            string     `json:"body"`
	Provenance      *string    `json:"provenance,omitempty"`
	CreatedAt       Timestamp  `json:"created_at"`
	UpdatedAt       Timestamp  `json:"updated_at"`
	SupersededBy    *ID        `json:"superseded_by,omitempty"`
	Tombstone       Tombstone  `json:"tombstoned"`
	CreationReplica ReplicaKey `json:"creation_replica"`
	Revision        Revision   `json:"revision"`
}

func (memory Memory) Validate() error {
	if err := memory.ID.Validate(); err != nil {
		return fieldError("Memory", "id", err)
	}
	if err := memory.ProjectKey.Validate(); err != nil {
		return fieldError("Memory", "project key", err)
	}
	if memory.Title == "" {
		return invalidValue("Memory", "title", memory.Title)
	}
	if memory.Body == "" {
		return invalidValue("Memory", "body", memory.Body)
	}
	if err := memory.CreatedAt.Validate(); err != nil {
		return fieldError("Memory", "created_at", err)
	}
	if err := memory.UpdatedAt.Validate(); err != nil {
		return fieldError("Memory", "updated_at", err)
	}
	if memory.SupersededBy != nil {
		if err := memory.SupersededBy.Validate(); err != nil {
			return fieldError("Memory", "superseded_by", err)
		}
		if *memory.SupersededBy == memory.ID {
			return fmt.Errorf("%w: a Memory cannot supersede itself", ErrInvalid)
		}
	}
	if err := memory.CreationReplica.Validate(); err != nil {
		return fieldError("Memory", "creation replica", err)
	}
	if err := memory.Revision.Validate(); err != nil {
		return fieldError("Memory", "revision", err)
	}
	return nil
}

// Replica is the local active writable-store lineage state. LastExportHead may
// be empty before the Replica's first export.
type Replica struct {
	Key            ReplicaKey `json:"replica_key"`
	LastExportHead string     `json:"last_export_head,omitempty"`
}

func (replica Replica) Validate() error { return replica.Key.Validate() }

// ReplicaHead records the accepted snapshot head for one imported Replica.
type ReplicaHead struct {
	Replica      ReplicaKey `json:"replica_key"`
	SnapshotHead string     `json:"snapshot_head"`
}

func (head ReplicaHead) Validate() error {
	if err := head.Replica.Validate(); err != nil {
		return fieldError("replica head", "replica key", err)
	}
	if head.SnapshotHead == "" {
		return invalidValue("replica head", "snapshot head", head.SnapshotHead)
	}
	return nil
}

// ReplicaSuccession records replacement of one active Replica key by another.
type ReplicaSuccession struct {
	OldReplica ReplicaKey `json:"old_replica_key"`
	NewReplica ReplicaKey `json:"new_replica_key"`
	RekeyedAt  Timestamp  `json:"rekeyed_at"`
}

func (succession ReplicaSuccession) Validate() error {
	if err := succession.OldReplica.Validate(); err != nil {
		return fieldError("replica succession", "old replica", err)
	}
	if err := succession.NewReplica.Validate(); err != nil {
		return fieldError("replica succession", "new replica", err)
	}
	if succession.OldReplica == succession.NewReplica {
		return fmt.Errorf("%w: Replica succession keys must differ", ErrInvalid)
	}
	if err := succession.RekeyedAt.Validate(); err != nil {
		return fieldError("replica succession", "rekeyed_at", err)
	}
	return nil
}

func validateOptionalTimestamp(value *Timestamp) error {
	if value == nil {
		return nil
	}
	return value.Validate()
}

func fieldError(record, field string, err error) error {
	return fmt.Errorf("%s %s: %w", record, field, err)
}

func invalidValue(record, field, value string) error {
	return fmt.Errorf("%w: %s %s %q", ErrInvalid, record, field, value)
}
