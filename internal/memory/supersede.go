package memory

import (
	"fmt"

	"github.com/tedkulp/drops/internal/model"
)

// Link is the minimum supersession state ValidateSupersession needs: one
// memory's identity, project, and successor. It is smaller than model.Memory
// so the validator stays pure and testable from literals, and so an import can
// pass a partial view before records are settled.
type Link struct {
	ID           model.ID
	Project      model.ProjectKey
	SupersededBy *model.ID
}

// ValidateSupersession checks the supersession edges among links and returns
// the first violation, wrapped in model.ErrInvalid. The invariants are:
//
//   - no memory supersedes itself;
//   - every successor names a memory in the set;
//   - a link never crosses a project boundary;
//   - following successors from any memory never loops back.
func ValidateSupersession(links []Link) error {
	index := make(map[model.ID]Link, len(links))
	for _, l := range links {
		index[l.ID] = l
	}

	for _, l := range links {
		if l.SupersededBy == nil {
			continue
		}
		if *l.SupersededBy == l.ID {
			return fmt.Errorf("%w: memory %s cannot supersede itself", model.ErrInvalid, l.ID)
		}
		target, ok := index[*l.SupersededBy]
		if !ok {
			return fmt.Errorf("%w: memory %s is superseded by %s, which is absent",
				model.ErrInvalid, l.ID, *l.SupersededBy)
		}
		if target.Project != l.Project {
			return fmt.Errorf("%w: memory %s and its successor %s are in different projects",
				model.ErrInvalid, l.ID, *l.SupersededBy)
		}
	}

	for _, l := range links {
		if err := walkChain(l, index); err != nil {
			return err
		}
	}
	return nil
}

// walkChain follows successors from start to a live head, reporting a cycle.
// Every target resolves because the structural pass above already rejected
// dangling links.
func walkChain(start Link, index map[model.ID]Link) error {
	seen := map[model.ID]bool{start.ID: true}
	for cur := start; cur.SupersededBy != nil; {
		next := index[*cur.SupersededBy]
		if seen[next.ID] {
			return fmt.Errorf("%w: supersession cycle involving %s", model.ErrInvalid, start.ID)
		}
		seen[next.ID] = true
		cur = next
	}
	return nil
}
