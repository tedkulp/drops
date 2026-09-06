package model

// IssueType is the closed Issue classification vocabulary.
type IssueType string

const (
	TypeTask     IssueType = "task"
	TypeBug      IssueType = "bug"
	TypeFeature  IssueType = "feature"
	TypeEpic     IssueType = "epic"
	TypeChore    IssueType = "chore"
	TypeResearch IssueType = "research"
	TypeDecision IssueType = "decision"
)

func ValidIssueType(value IssueType) bool {
	switch value {
	case TypeTask, TypeBug, TypeFeature, TypeEpic, TypeChore, TypeResearch, TypeDecision:
		return true
	default:
		return false
	}
}

// Status is an Issue's stored lifecycle. Blocking and tombstoning are modeled
// independently and therefore are not statuses.
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusClosed     Status = "closed"
)

func ValidStatus(value Status) bool {
	switch value {
	case StatusOpen, StatusInProgress, StatusClosed:
		return true
	default:
		return false
	}
}

func (status Status) IsTerminal() bool { return status == StatusClosed }

// DependencyType is the closed generic relationship vocabulary. Parentage has
// its own authoritative record and is not a DependencyType.
type DependencyType string

const (
	DepBlocks         DependencyType = "blocks"
	DepRelated        DependencyType = "related"
	DepDiscoveredFrom DependencyType = "discovered-from"
)

// Undirected reports whether an edge of this type means the same relation read
// from either end. `related` is the only one: `blocks` and `discovered-from`
// name a different thing at each end, and a page names both. So `A related B`
// and `B related A` are one relation spelled two ways, while `A blocks B` and
// `B blocks A` are two edges and a cycle.
func (value DependencyType) Undirected() bool { return value == DepRelated }

func ValidDependencyType(value DependencyType) bool {
	switch value {
	case DepBlocks, DepRelated, DepDiscoveredFrom:
		return true
	default:
		return false
	}
}

func ValidPriority(priority int) bool { return priority >= 0 && priority <= 4 }

// OwnerKind reserves an opaque ID permanently to one entity kind.
type OwnerKind string

const (
	OwnerIssue  OwnerKind = "issue"
	OwnerMemory OwnerKind = "memory"
)

func ValidOwnerKind(value OwnerKind) bool {
	return value == OwnerIssue || value == OwnerMemory
}
