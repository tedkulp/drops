package core

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/store"
)

// IssueRef names one related Issue by what a reader sees beside it: the ID it
// is addressed by, and the title, status and tombstone state printed with it.
//
// A relation record carries IDs and nothing else, so a caller handed those raw
// has to read every far end for itself — which is exactly the N+1 ViewIssue's
// transaction exists to prevent, and which no promise about one revision can
// survive.
type IssueRef struct {
	ID         model.ID     `json:"id"`
	Title      string       `json:"title"`
	Status     model.Status `json:"status"`
	Tombstoned bool         `json:"tombstoned"`
}

// DependencyRef is an IssueRef reached over a typed edge. The type travels with
// the far end because it is the only thing separating a blocker from a merely
// related Issue, and a caller that had to recover it would be reading the edge
// a second time.
type DependencyRef struct {
	IssueRef
	Type model.DependencyType `json:"dep_type"`
}

// IssueView is one coherent reading-page snapshot: the Issue, its Project, its
// labels and thread, and every relation already resolved to the far end a
// reader sees.
type IssueView struct {
	Issue        model.Issue     `json:"issue"`
	Project      model.Project   `json:"project"`
	Parent       *IssueRef       `json:"parent,omitempty"`
	Children     []IssueRef      `json:"children"`
	Dependencies []DependencyRef `json:"dependencies"`
	Dependents   []DependencyRef `json:"dependents"`
	Labels       []model.Label   `json:"labels"`
	Comments     []model.Comment `json:"comments"`
}

// ViewIssue reads an Issue and every record rendered with it in one
// transaction, so a page cannot combine two revisions of the graph. That
// includes the title and status of every relation: those are read here, in the
// same transaction, rather than left to the caller as a list of IDs to chase.
func (core *Core) ViewIssue(ctx context.Context, id model.ID) (IssueView, error) {
	var view IssueView
	err := core.store.WithTx(ctx, func(ctx context.Context, tx *store.Tx) error {
		issue, err := tx.Issue(ctx, id)
		if err != nil {
			return err
		}
		project, err := tx.Project(ctx, issue.ProjectKey)
		if err != nil {
			return err
		}
		var parentID *model.ID
		parent, err := tx.IssueParent(ctx, id)
		if err == nil && parent.Tombstone == model.Live {
			parentID = &parent.ParentID
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		children, err := tx.IssueChildren(ctx, id)
		if err != nil {
			return err
		}
		dependencies, err := tx.DependenciesFrom(ctx, id)
		if err != nil {
			return err
		}
		dependents, err := tx.DependenciesTo(ctx, id)
		if err != nil {
			return err
		}
		labels, err := tx.IssueLabels(ctx, id)
		if err != nil {
			return err
		}
		comments, err := tx.IssueComments(ctx, id)
		if err != nil {
			return err
		}

		// The far ends, in this same transaction and in one query. The
		// resolver takes the transaction rather than the Store so that a
		// later edit cannot quietly move these reads back outside it.
		related := make([]model.ID, 0, 1+len(children)+len(dependencies)+len(dependents))
		if parentID != nil {
			related = append(related, *parentID)
		}
		for _, child := range children {
			related = append(related, child.ChildID)
		}
		for _, dependency := range dependencies {
			related = append(related, dependency.ToID)
		}
		for _, dependent := range dependents {
			related = append(related, dependent.FromID)
		}
		refs, err := tx.IssuesByID(ctx, related)
		if err != nil {
			return err
		}

		var parentRef *IssueRef
		if parentID != nil {
			ref := issueRef(refs[*parentID])
			parentRef = &ref
		}
		childRefs := make([]IssueRef, 0, len(children))
		for _, child := range children {
			childRefs = append(childRefs, issueRef(refs[child.ChildID]))
		}
		// A dependency names the Issue at the OTHER end of its edge: the one
		// this Issue points at going out, and the one pointing at it coming in.
		dependencyRefs := make([]DependencyRef, 0, len(dependencies))
		for _, dependency := range dependencies {
			dependencyRefs = append(dependencyRefs, DependencyRef{
				IssueRef: issueRef(refs[dependency.ToID]), Type: dependency.Type,
			})
		}
		dependentRefs := make([]DependencyRef, 0, len(dependents))
		for _, dependent := range dependents {
			dependentRefs = append(dependentRefs, DependencyRef{
				IssueRef: issueRef(refs[dependent.FromID]), Type: dependent.Type,
			})
		}
		sortIssueRefs(childRefs)
		sortDependencyRefs(dependencyRefs)
		sortDependencyRefs(dependentRefs)

		view.Issue, view.Project, view.Parent = issue, project, parentRef
		view.Children = childRefs
		view.Dependencies, view.Dependents = dependencyRefs, dependentRefs
		view.Labels, view.Comments = labels, comments
		return nil
	})
	if view.Children == nil {
		view.Children = []IssueRef{}
	}
	if view.Dependencies == nil {
		view.Dependencies = []DependencyRef{}
	}
	if view.Dependents == nil {
		view.Dependents = []DependencyRef{}
	}
	if view.Labels == nil {
		view.Labels = []model.Label{}
	}
	if view.Comments == nil {
		view.Comments = []model.Comment{}
	}
	return view, err
}

// issueRef reduces a related Issue to the fields a reader is shown. A tombstone
// is a state, not an absence: the relation is still listed, and it is marked.
func issueRef(issue model.Issue) IssueRef {
	return IssueRef{
		ID:         issue.ID,
		Title:      issue.Title,
		Status:     issue.Status,
		Tombstoned: issue.Tombstone == model.Tombstoned,
	}
}

// sortIssueRefs puts a relation block in the order a reader reads IDs, which is
// not the lexicographic order the store returns: `<p>.2` precedes `<p>.10`. The
// order is settled here, once, so a page and a pane cannot disagree about it.
func sortIssueRefs(refs []IssueRef) {
	slices.SortStableFunc(refs, func(a, b IssueRef) int { return compareIDs(a.ID, b.ID) })
}

// sortDependencyRefs groups a dependency block by edge type before ordering the
// far ends, so blockers stay together however their IDs sort.
func sortDependencyRefs(refs []DependencyRef) {
	slices.SortStableFunc(refs, func(a, b DependencyRef) int {
		if a.Type != b.Type {
			return strings.Compare(string(a.Type), string(b.Type))
		}
		return compareIDs(a.ID, b.ID)
	})
}

func compareIDs(a, b model.ID) int {
	switch {
	case naturalLess(string(a), string(b)):
		return -1
	case naturalLess(string(b), string(a)):
		return 1
	default:
		return 0
	}
}

// naturalLess compares two ids as alternating runs of digits and non-digits,
// comparing digit runs numerically, so `<p>.2` precedes `<p>.10`.
func naturalLess(a, b string) bool {
	as, bs := idRuns(a), idRuns(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, xErr := strconv.Atoi(as[i])
		y, yErr := strconv.Atoi(bs[i])
		if xErr == nil && yErr == nil {
			if x != y {
				return x < y
			}
			continue
		}
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

func idRuns(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		j, digit := i, isDigit(s[i])
		for j < len(s) && isDigit(s[j]) == digit {
			j++
		}
		out = append(out, s[i:j])
		i = j
	}
	return out
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
