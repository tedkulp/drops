package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func registerLabelCmds(root *cobra.Command, app *App) {
	label := &cobra.Command{Use: "label", Short: "Manage issue labels"}
	label.AddCommand(
		newLabelAddCmd(app),
		newLabelRmCmd(app),
		newLabelListCmd(app),
	)
	root.AddCommand(label)
}

func newLabelAddCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "add <id> <label>...",
		Short: "Add labels to an issue",
		Args:  minimumArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			id := model.ID(args[0])
			for _, name := range args[1:] {
				if _, err := app.core.SetLabel(app.ctx, id, name, true); err != nil {
					return err
				}
			}
			if !app.json {
				fmt.Fprintf(app.out, "labelled %s: %v\n", id, args[1:])
			}
			return nil
		},
	}
}

func newLabelRmCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id> <label>...",
		Short: "Remove labels from an issue",
		Args:  minimumArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			id := model.ID(args[0])
			for _, name := range args[1:] {
				if _, err := app.core.SetLabel(app.ctx, id, name, false); err != nil {
					return err
				}
			}
			if !app.json {
				fmt.Fprintf(app.out, "removed from %s: %v\n", id, args[1:])
			}
			return nil
		},
	}
}

func newLabelListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list [<id>]",
		Short: "List an issue's labels, or every label with its issue count",
		Args:  maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				labels, err := app.core.IssueLabels(app.ctx, model.ID(args[0]))
				if err != nil {
					return err
				}
				names := liveLabelNames(labels)
				if app.json {
					return render.EmitMany(app.out, names)
				}
				for _, n := range names {
					fmt.Fprintln(app.out, n)
				}
				return nil
			}
			counts, err := app.storeWideLabelCounts()
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, counts)
			}
			names := make([]string, 0, len(counts))
			for n := range counts {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				fmt.Fprintf(app.out, "%-24s %d\n", n, counts[n])
			}
			return nil
		},
	}
}

func liveLabelNames(labels []model.Label) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l.Tombstone != model.Tombstoned {
			out = append(out, l.Name)
		}
	}
	return out
}

// storeWideLabelCounts counts live labels across every issue, ignoring -P and
// the ambient project, which is the documented (and rough-edged) behaviour of
// `label list` with no id.
func (a *App) storeWideLabelCounts() (map[string]int, error) {
	issues, err := a.core.Issues(a.ctx, core.IssueFilter{IncludeTombstoned: true})
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, iss := range issues {
		labels, err := a.core.IssueLabels(a.ctx, iss.ID)
		if err != nil {
			return nil, err
		}
		for _, name := range liveLabelNames(labels) {
			counts[name]++
		}
	}
	return counts, nil
}
