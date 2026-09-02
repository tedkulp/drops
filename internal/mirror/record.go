package mirror

import (
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

// Record is one typed wire record. Kind names the record shape, and Value holds
// the concrete model record whose type matches Kind.
type Record struct {
	Kind  model.RecordKind `json:"kind"`
	Value any              `json:"record"`
}

// sep joins the components of a composite record key. NUL cannot appear in a
// JSON string on the wire (it must be escaped), so no component can collide
// with the separator.
const sep = "\x00"

// NewRecord wraps a model record, deriving its Kind from the concrete type.
// Values and non-nil pointers to the nine replicated record types are accepted;
// a pointer is dereferenced so Value always holds a value.
func NewRecord(v any) (Record, error) {
	value, ok := deref(v)
	if !ok {
		return Record{}, fmt.Errorf("%w: unsupported record type %T", model.ErrInvalid, v)
	}
	kind, ok := kindOf(value)
	if !ok {
		return Record{}, fmt.Errorf("%w: unsupported record type %T", model.ErrInvalid, v)
	}
	return Record{Kind: kind, Value: value}, nil
}

// Ref returns the record's printable mirror identity. The key is the canonical
// composite encoding mirror owns.
func (record Record) Ref() (model.RecordRef, error) {
	value, err := record.validatedValue()
	if err != nil {
		return model.RecordRef{}, err
	}
	return model.RecordRef{Kind: record.Kind, Key: keyOf(value)}, nil
}

// Validate rejects a record whose Kind and value type disagree, or whose value
// fails model-level validation.
func (record Record) Validate() error {
	value, err := record.validatedValue()
	if err != nil {
		return err
	}
	switch v := value.(type) {
	case model.Project:
		return v.Validate()
	case model.RepositoryLocator:
		return v.Validate()
	case model.Issue:
		return v.Validate()
	case model.IssueParent:
		return v.Validate()
	case model.Dependency:
		return v.Validate()
	case model.Label:
		return v.Validate()
	case model.Comment:
		return v.Validate()
	case model.Memory:
		return v.Validate()
	case model.ReplicaSuccession:
		return v.Validate()
	}
	return fmt.Errorf("%w: unsupported record type %T", model.ErrInvalid, value)
}

// validatedValue dereferences Value and confirms its concrete type matches Kind.
func (record Record) validatedValue() (any, error) {
	value, ok := deref(record.Value)
	if !ok {
		return nil, fmt.Errorf("%w: nil or unsupported record value", model.ErrInvalid)
	}
	kind, ok := kindOf(value)
	if !ok {
		return nil, fmt.Errorf("%w: unsupported record type %T", model.ErrInvalid, record.Value)
	}
	if kind != record.Kind {
		return nil, fmt.Errorf("%w: record kind %q does not match value type %T", model.ErrInvalid, record.Kind, record.Value)
	}
	return value, nil
}

// keyOf returns the canonical composite key for a record value. It is only
// called after validatedValue has confirmed the concrete type.
func keyOf(value any) string {
	switch v := value.(type) {
	case model.Project:
		return string(v.Key)
	case model.RepositoryLocator:
		return string(v.ProjectKey) + sep + v.Locator
	case model.Issue:
		return string(v.ID)
	case model.IssueParent:
		return string(v.ChildID)
	case model.Dependency:
		return string(v.FromID) + sep + string(v.ToID) + sep + string(v.Type)
	case model.Label:
		return string(v.IssueID) + sep + v.Name
	case model.Comment:
		return string(v.ID)
	case model.Memory:
		return string(v.ID)
	case model.ReplicaSuccession:
		return string(v.OldReplica) + sep + string(v.NewReplica)
	}
	panic("mirror: keyOf for non-record value")
}

// deref normalizes a model record (value or non-nil pointer) to its value form.
func deref(v any) (any, bool) {
	switch p := v.(type) {
	case *model.Project:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.RepositoryLocator:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.Issue:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.IssueParent:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.Dependency:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.Label:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.Comment:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.Memory:
		if p == nil {
			return nil, false
		}
		return *p, true
	case *model.ReplicaSuccession:
		if p == nil {
			return nil, false
		}
		return *p, true
	default:
		return v, true
	}
}

// kindOf reports the RecordKind for a concrete model record value.
func kindOf(v any) (model.RecordKind, bool) {
	switch v.(type) {
	case model.Project:
		return model.RecordProject, true
	case model.RepositoryLocator:
		return model.RecordRepositoryLocator, true
	case model.Issue:
		return model.RecordIssue, true
	case model.IssueParent:
		return model.RecordIssueParent, true
	case model.Dependency:
		return model.RecordDependency, true
	case model.Label:
		return model.RecordLabel, true
	case model.Comment:
		return model.RecordComment, true
	case model.Memory:
		return model.RecordMemory, true
	case model.ReplicaSuccession:
		return model.RecordReplicaSuccession, true
	}
	return "", false
}
