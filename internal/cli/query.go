// Package cli query verbs: list, ready, blocked, count, search.
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func registerQueryCmds(root *cobra.Command, app *App) {
	root.AddCommand(
		newListCmd(app),
		newReadyCmd(app),
		newBlockedCmd(app),
		newCountCmd(app),
		newSearchCmd(app),
	)
}

// scopedIssueFilter builds the shared read filter. When the directory resolves
// to no project and --all-projects was not given, it returns ok=false so the
// caller emits an empty result rather than every project's issues.
func scopedIssueFilter(app *App, limit int) (core.IssueFilter, bool, error) {
	f := core.IssueFilter{Limit: limit}
	if app.allProjects {
		return f, true, nil
	}
	scoped, found, err := app.scopedProject(false)
	if err != nil || !found {
		return f, false, err
	}
	key := scoped.Key
	f.Project = &key
	return f, true, nil
}

// applyStatuses resolves the --status and -a flags into a status set and a
// tombstone toggle. "" statuses with no -a mean the active statuses; -a adds
// the terminal one; the literal "all" means every status including tombstones.
func applyStatuses(f *core.IssueFilter, statuses []string, includeClosed bool) error {
	switch {
	case len(statuses) == 1 && statuses[0] == "all":
		f.IncludeTombstoned = true
		f.Statuses = nil
	case len(statuses) > 0:
		for _, s := range statuses {
			st := model.Status(s)
			if !model.ValidStatus(st) {
				return invalidArgs("unknown status %q", s)
			}
			f.Statuses = append(f.Statuses, st)
		}
	case includeClosed:
		f.Statuses = nil // every live status, tombstones still excluded
	default:
		f.Statuses = []model.Status{model.StatusOpen, model.StatusInProgress}
	}
	return nil
}

func toTypes(types []string) []model.IssueType {
	out := make([]model.IssueType, 0, len(types))
	for _, t := range types {
		out = append(out, model.IssueType(t))
	}
	return out
}

func addTypeLimitFlags(cmd *cobra.Command, types *[]string, limit *int, limitShorthand string) {
	f := cmd.Flags()
	f.StringSliceVarP(types, "type", "t", nil, "filter by issue type (repeatable)")
	f.IntVarP(limit, "limit", limitShorthand, 0, "maximum results (0 = unlimited)")
	registerIssueTypeCompletion(cmd)
}

func addStatusFlag(cmd *cobra.Command, statuses *[]string) {
	cmd.Flags().StringSliceVarP(statuses, "status", "s", nil,
		`filter by status (repeatable); "all" means every status including tombstones`)
	registerStatusCompletion(cmd, true)
}

func rejectIncludeClosed(app *App) error {
	if !app.includeClosed {
		return nil
	}
	return invalidArgs("-a/--all is meaningless here - a ready or blocked issue is open " +
		"or in_progress by definition; use `list -a` to see closed issues")
}

// slugMap caches project-key to slug for the --all-projects column.
func (a *App) slugMap() (map[model.ProjectKey]string, error) {
	projects, err := a.core.Projects(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[model.ProjectKey]string, len(projects))
	for _, p := range projects {
		out[p.Key] = p.Slug
	}
	return out, nil
}

func rowsForIssues(app *App, issues []model.Issue, noteFn func(w *strings.Builder, iss model.Issue)) error {
	var slugs map[model.ProjectKey]string
	if app.allProjects {
		var err error
		slugs, err = app.slugMap()
		if err != nil {
			return err
		}
	}
	listing := render.Listing{Project: app.allProjects}
	for _, iss := range issues {
		row := render.Row{
			ID:         iss.ID,
			Status:     iss.Status,
			Type:       iss.Type,
			Priority:   iss.Priority,
			Title:      iss.Title,
			Tombstoned: iss.Tombstone == model.Tombstoned,
		}
		if slugs != nil {
			row.Project = slugs[iss.ProjectKey]
		}
		if noteFn != nil {
			var b strings.Builder
			noteFn(&b, iss)
			row.Note = strings.TrimRight(b.String(), "\n")
		}
		listing.Rows = append(listing.Rows, row)
	}
	return newConsole(app.out, app.err).Rows(listing)
}

func emitIssues(app *App, issues []model.Issue) error {
	if app.json {
		return render.EmitMany(app.out, issues)
	}
	return rowsForIssues(app, issues, nil)
}

// isDeferred lives at the CLI because only it compares DeferredUntil against
// "now"; the store filter has no clock.
func isDirectChildID(id, parentID model.ID) bool {
	tail, ok := strings.CutPrefix(string(id), string(parentID)+".")
	return ok && !strings.Contains(tail, ".")
}

func newListCmd(app *App) *cobra.Command {
	var (
		statuses, types      []string
		labelsAll, labelsAny []string
		limit                int
		deferred             bool
		parent               string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues in the current project (--all-projects spans every project)",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			parentGiven := cmd.Flags().Changed("parent")
			if parentGiven && parent == "" {
				return invalidArgs("--parent needs an issue id; an empty value would list every issue")
			}
			f, ok, err := scopedIssueFilter(app, 0)
			if err != nil || !ok {
				if err != nil {
					return err
				}
				return emitIssues(app, nil)
			}
			f.Types = toTypes(types)
			if err := applyStatuses(&f, statuses, app.includeClosed); err != nil {
				return err
			}
			if len(labelsAny) > 0 {
				f.AnyLabels = labelsAny
			}
			issues, err := app.core.Issues(app.ctx, f)
			if err != nil {
				return err
			}
			if len(labelsAll) > 0 {
				issues, err = app.filterAllLabels(issues, labelsAll)
				if err != nil {
					return err
				}
			}
			if deferred {
				issues = app.filterDeferred(issues)
			}
			if parentGiven {
				pid := model.ID(parent)
				kept := issues[:0]
				for _, iss := range issues {
					if isDirectChildID(iss.ID, pid) {
						kept = append(kept, iss)
					}
				}
				issues = kept
			}
			if limit > 0 && len(issues) > limit {
				issues = issues[:limit]
			}
			return emitIssues(app, issues)
		},
	}
	addTypeLimitFlags(cmd, &types, &limit, "")
	addStatusFlag(cmd, &statuses)
	f := cmd.Flags()
	f.StringSliceVarP(&labelsAll, "label", "l", nil, "issue must carry every one of these labels")
	f.StringSliceVar(&labelsAny, "label-any", nil, "issue must carry at least one of these labels")
	f.BoolVar(&deferred, "deferred", false,
		"only issues deferred into the future (compared against now, not merely non-null)")
	f.StringVar(&parent, "parent", "",
		"only direct children of this issue id (a further \".\" in the tail is a grandchild, excluded)")
	registerDynamicFlagCompletion(cmd, "label", completeLabels(app))
	registerDynamicFlagCompletion(cmd, "label-any", completeLabels(app))
	registerDynamicFlagCompletion(cmd, "parent", completeIssueIDsForFlag(app))
	return cmd
}

func (a *App) filterAllLabels(issues []model.Issue, want []string) ([]model.Issue, error) {
	kept := make([]model.Issue, 0, len(issues))
	for _, iss := range issues {
		labels, err := a.core.IssueLabels(a.ctx, iss.ID)
		if err != nil {
			return nil, err
		}
		have := map[string]bool{}
		for _, l := range labels {
			have[l.Name] = true
		}
		all := true
		for _, w := range want {
			if !have[w] {
				all = false
				break
			}
		}
		if all {
			kept = append(kept, iss)
		}
	}
	return kept, nil
}

func (a *App) filterDeferred(issues []model.Issue) []model.Issue {
	kept := make([]model.Issue, 0, len(issues))
	now := time.Now()
	for _, iss := range issues {
		if iss.DeferredUntil == nil {
			continue
		}
		until, err := iss.DeferredUntil.Time()
		if err == nil && until.After(now) {
			kept = append(kept, iss)
		}
	}
	return kept
}

func newReadyCmd(app *App) *cobra.Command {
	var (
		types []string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "List actionable issues: open or in_progress, not deferred, no open blockers",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rejectIncludeClosed(app); err != nil {
				return err
			}
			f, ok, err := scopedIssueFilter(app, limit)
			if err != nil || !ok {
				if err != nil {
					return err
				}
				return emitIssues(app, nil)
			}
			f.Types = toTypes(types)
			issues, err := app.core.Ready(app.ctx, f)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitMany(app.out, issues)
			}
			ids := make([]model.ID, len(issues))
			for i, iss := range issues {
				ids[i] = iss.ID
			}
			impacts, err := app.core.UnblockImpacts(app.ctx, ids)
			if err != nil {
				return err
			}
			return rowsForIssues(app, issues, func(w *strings.Builder, iss model.Issue) {
				if n := impacts[iss.ID]; n > 0 {
					fmt.Fprintf(w, "unblocks %d", n)
				}
			})
		},
	}
	addTypeLimitFlags(cmd, &types, &limit, "n")
	return cmd
}

func newBlockedCmd(app *App) *cobra.Command {
	var (
		types []string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "blocked",
		Short: "List live issues waiting on an unfinished blocker",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rejectIncludeClosed(app); err != nil {
				return err
			}
			f, ok, err := scopedIssueFilter(app, limit)
			if err != nil || !ok {
				if err != nil {
					return err
				}
				if app.json {
					return render.EmitMany(app.out, []core.BlockedIssue{})
				}
				return nil
			}
			f.Types = toTypes(types)
			blocked, err := app.core.Blocked(app.ctx, f)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitMany(app.out, blocked)
			}
			issues := make([]model.Issue, len(blocked))
			by := make(map[model.ID][]model.ID, len(blocked))
			for i, b := range blocked {
				issues[i] = b.Issue
				by[b.Issue.ID] = b.BlockedBy
			}
			return rowsForIssues(app, issues, func(w *strings.Builder, iss model.Issue) {
				names := make([]string, len(by[iss.ID]))
				for i, id := range by[iss.ID] {
					names[i] = string(id)
				}
				fmt.Fprintf(w, "blocked by %s", strings.Join(names, ", "))
			})
		},
	}
	addTypeLimitFlags(cmd, &types, &limit, "")
	return cmd
}

func newCountCmd(app *App) *cobra.Command {
	var statuses, types []string
	cmd := &cobra.Command{
		Use:   "count",
		Short: "Count issues matching the current scope and filters",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, ok, err := scopedIssueFilter(app, 0)
			if err != nil {
				return err
			}
			n := 0
			if ok {
				f.Types = toTypes(types)
				if err := applyStatuses(&f, statuses, app.includeClosed); err != nil {
					return err
				}
				issues, err := app.core.Issues(app.ctx, f)
				if err != nil {
					return err
				}
				n = len(issues)
			}
			if app.json {
				return render.EmitOne(app.out, map[string]int{"count": n})
			}
			fmt.Fprintln(app.out, n)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVarP(&types, "type", "t", nil, "filter by issue type (repeatable)")
	addStatusFlag(cmd, &statuses)
	return cmd
}

func newSearchCmd(app *App) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search issue titles, descriptions and comment threads in the current scope",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, ok, err := scopedIssueFilter(app, limit)
			if err != nil || !ok {
				if err != nil {
					return err
				}
				return emitIssues(app, nil)
			}
			// -a means the same here as on every other scanning verb.
			// Without this search answered from every live status, so the
			// flag an agent passes to reach closed issues was inert and the
			// default was wider than list's.
			if err := applyStatuses(&f, nil, app.includeClosed); err != nil {
				return err
			}
			issues, err := app.core.SearchIssues(app.ctx, args[0], f)
			if err != nil {
				return err
			}
			return emitIssues(app, issues)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum results (0 = unlimited)")
	return cmd
}
