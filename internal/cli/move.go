package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

// moveEdge is one relationship that crosses a project boundary because of a
// move.
type moveEdge struct {
	FromID      string `json:"from_id"`
	FromProject string `json:"from_project"`
	ToID        string `json:"to_id"`
	ToProject   string `json:"to_project"`
}

// moveResult is the full account of a move, matching the documented --json
// shape: moved, noop, crossing_blocks, crossing_parentage, to, committed.
type moveResult struct {
	Moved             []model.Issue `json:"moved"`
	NoOp              []model.Issue `json:"noop"`
	CrossingBlocks    []moveEdge    `json:"crossing_blocks"`
	CrossingParentage []moveEdge    `json:"crossing_parentage"`
	To                string        `json:"to"`
	Committed         bool          `json:"committed"`
}

func registerMoveCmds(root *cobra.Command, app *App) {
	root.AddCommand(newMoveCmd(app))
}

func newMoveCmd(app *App) *cobra.Command {
	var (
		to      string
		subtree bool
		dryRun  bool
	)
	cmd := &cobra.Command{
		Use:   "move <id>... --to <slug>",
		Short: "Move issues to another project",
		Long: "Move issues to another project, which must already exist.\n\n" +
			"IDS ARE NEVER REWRITTEN: an id is unique across the whole store, and its\n" +
			"prefix records where it was minted, not where it lives.\n\n" +
			"The destination is spelled --to, not -P.",
		Args:              minimumArgs(1),
		ValidArgsFunction: completeIssueIDs(app, -1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			if !cmd.Flags().Changed("to") {
				return invalidArgs("--to <slug> is required")
			}
			dest, err := app.core.ProjectBySlug(app.ctx, to)
			if err != nil {
				return err
			}

			ids := make([]model.ID, 0, len(args))
			seen := map[model.ID]bool{}
			add := func(id model.ID) {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			for _, raw := range args {
				add(model.ID(raw))
			}
			if subtree {
				for _, id := range append([]model.ID(nil), ids...) {
					descendants, err := app.descendants(id)
					if err != nil {
						return err
					}
					for _, d := range descendants {
						add(d)
					}
				}
			}

			res := moveResult{Committed: !dryRun, To: to}
			var movedIDs []model.ID
			for _, id := range ids {
				issue, err := app.core.Issue(app.ctx, id)
				if err != nil {
					return err
				}
				if issue.ProjectKey == dest.Key {
					res.NoOp = append(res.NoOp, issue)
				} else {
					res.Moved = append(res.Moved, issue)
					movedIDs = append(movedIDs, id)
				}
			}

			res.CrossingBlocks, res.CrossingParentage, err = app.moveCrossings(movedIDs, seen, dest.Key)
			if err != nil {
				return err
			}

			if !dryRun && len(movedIDs) > 0 {
				if _, err := app.core.MoveIssues(app.ctx, movedIDs, dest.Key); err != nil {
					return err
				}
			}

			if app.json {
				return render.EmitOne(app.out, res)
			}
			renderMoveResult(app, res)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&to, "to", "", "destination project slug (required)")
	f.BoolVar(&subtree, "subtree", false, "also move every descendant")
	f.BoolVar(&dryRun, "dry-run", false, "run the real write path and roll it back")
	registerDynamicFlagCompletion(cmd, "to", completeProjectSlugs(app, false))
	return cmd
}

// descendants returns every issue whose parentage reaches id, transitively.
func (a *App) descendants(id model.ID) ([]model.ID, error) {
	var out []model.ID
	seen := map[model.ID]bool{}
	var walk func(model.ID) error
	walk = func(parent model.ID) error {
		children, err := a.core.IssueChildren(a.ctx, parent)
		if err != nil {
			return err
		}
		for _, child := range children {
			if seen[child.ChildID] {
				continue
			}
			seen[child.ChildID] = true
			out = append(out, child.ChildID)
			if err := walk(child.ChildID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(id); err != nil {
		return nil, err
	}
	return out, nil
}

// moveCrossings reports blocks and parentage edges that a move splits across
// projects: one endpoint is in the moving set, the other is not.
//
// It runs BEFORE the write, so an endpoint's stored project is still where it
// came from. An endpoint in the moving set therefore reports the DESTINATION,
// which is the only way the report can show a crossing at all: reading both
// ends out of the store here printed the same project on both sides of every
// edge, so a list whose whole purpose is to name a boundary named none.
// --dry-run prints what a real move would print, which is the same answer.
func (a *App) moveCrossings(movedIDs []model.ID, moving map[model.ID]bool, dest model.ProjectKey) ([]moveEdge, []moveEdge, error) {
	slugs, err := a.slugMap()
	if err != nil {
		return nil, nil, err
	}
	projectOf := func(id model.ID) string {
		if moving[id] {
			return slugs[dest]
		}
		return slugs[issueProject(a, id)]
	}
	var blocks, parentage []moveEdge
	// One endpoint moving is not enough to make an edge cross: the other end
	// may already be in the destination, or be moving with it, in which case
	// the move JOINED them rather than splitting them. Comparing the two
	// answers is what makes "crossing" mean crossing — and it subsumes the
	// "is the other end in the moving set" test, since both ends of such an
	// edge answer with the destination.
	addEdge := func(from, to model.ID, list *[]moveEdge) {
		fromProject, toProject := projectOf(from), projectOf(to)
		if fromProject == toProject {
			return
		}
		*list = append(*list, moveEdge{
			FromID: string(from), FromProject: fromProject,
			ToID: string(to), ToProject: toProject,
		})
	}
	for _, id := range movedIDs {
		from, err := a.core.DependenciesFrom(a.ctx, id)
		if err != nil {
			return nil, nil, err
		}
		for _, dep := range from {
			if dep.Type != model.DepBlocks {
				continue
			}
			addEdge(id, dep.ToID, &blocks)
		}
		toDeps, err := a.core.DependenciesTo(a.ctx, id)
		if err != nil {
			return nil, nil, err
		}
		for _, dep := range toDeps {
			if dep.Type != model.DepBlocks {
				continue
			}
			addEdge(dep.FromID, id, &blocks)
		}
		// Parentage: the moved issue's parent, and its children.
		view, err := a.core.ViewIssue(a.ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if view.Parent != nil {
			addEdge(id, view.Parent.ID, &parentage)
		}
		for _, child := range view.Children {
			addEdge(child.ID, id, &parentage)
		}
	}
	return blocks, parentage, nil
}

func issueProject(a *App, id model.ID) model.ProjectKey {
	issue, err := a.core.Issue(a.ctx, id)
	if err != nil {
		return ""
	}
	return issue.ProjectKey
}

func renderMoveResult(app *App, res moveResult) {
	if !res.Committed {
		fmt.Fprintln(app.out, "dry run: nothing was written")
	}
	if len(res.Moved) == 0 && len(res.NoOp) == 0 {
		fmt.Fprintln(app.out, "nothing to move")
		return
	}
	if len(res.Moved) > 0 {
		fmt.Fprintf(app.out, "moved %d issue%s to %s\n", len(res.Moved), plural(len(res.Moved)), res.To)
		for _, m := range res.Moved {
			fmt.Fprintf(app.out, "  %-16s %s\n", m.ID, m.Title)
		}
	}
	if len(res.NoOp) > 0 {
		fmt.Fprintf(app.out, "already in %s (%d)\n", res.To, len(res.NoOp))
		for _, m := range res.NoOp {
			fmt.Fprintf(app.out, "  %-16s %s\n", m.ID, m.Title)
		}
	}
	if len(res.CrossingBlocks) > 0 {
		fmt.Fprintf(app.out, "crossing blocks (%d)\n", len(res.CrossingBlocks))
		for _, e := range res.CrossingBlocks {
			fmt.Fprintf(app.out, "  %s (%s) is blocked by %s (%s)\n", e.FromID, e.FromProject, e.ToID, e.ToProject)
		}
	}
	if len(res.CrossingParentage) > 0 {
		fmt.Fprintf(app.out, "crossing parentage (%d)\n", len(res.CrossingParentage))
		for _, e := range res.CrossingParentage {
			fmt.Fprintf(app.out, "  %s (%s) has parent %s (%s)\n", e.FromID, e.FromProject, e.ToID, e.ToProject)
		}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
