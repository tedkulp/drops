package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/model"
	"github.com/tedkulp/drops/internal/render"
)

func registerDepCmds(root *cobra.Command, app *App) {
	dep := &cobra.Command{Use: "dep", Short: "Manage dependencies between issues"}
	dep.AddCommand(
		newDepAddCmd(app),
		newDepRmCmd(app),
		newDepTreeCmd(app),
		newDepCyclesCmd(app),
	)
	root.AddCommand(dep)
}

func parseDepType(depType string) (model.DependencyType, error) {
	t := model.DependencyType(depType)
	if !model.ValidDependencyType(t) {
		return "", invalidArgs("dependency type must be blocks|related|discovered-from")
	}
	return t, nil
}

func newDepAddCmd(app *App) *cobra.Command {
	var depType string
	cmd := &cobra.Command{
		Use:   "add <id> <blocker-id>",
		Short: "Record that <id> depends on <blocker-id>",
		Long:  "Record that <id> depends on <blocker-id>. With the default type\n\"blocks\", <blocker-id> blocks <id>.",
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			t, err := parseDepType(depType)
			if err != nil {
				return err
			}
			if _, err := app.core.SetDependency(app.ctx, model.ID(args[0]), model.ID(args[1]), t, true); err != nil {
				return err
			}
			if !app.json {
				fmt.Fprintf(app.out, "%s now depends on %s (%s)\n", args[0], args[1], t)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&depType, "type", string(model.DepBlocks),
		"dependency type: blocks|related|discovered-from")
	return cmd
}

func newDepRmCmd(app *App) *cobra.Command {
	var depType string
	cmd := &cobra.Command{
		Use:   "rm <id> <blocker-id>",
		Short: "Remove a dependency edge",
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.ensureReplica(); err != nil {
				return err
			}
			t, err := parseDepType(depType)
			if err != nil {
				return err
			}
			if _, err := app.core.SetDependency(app.ctx, model.ID(args[0]), model.ID(args[1]), t, false); err != nil {
				return err
			}
			if !app.json {
				fmt.Fprintf(app.out, "removed %s -> %s (%s)\n", args[0], args[1], t)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&depType, "type", string(model.DepBlocks), "dependency type")
	return cmd
}

func newDepTreeCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "tree <id>",
		Short: "Show the longest chain of open blockers, deepest last",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chain, err := app.core.LongestBlockerChain(app.ctx, model.ID(args[0]))
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitMany(app.out, chain)
			}
			for i, id := range chain {
				fmt.Fprintf(app.out, "%s%s\n", indent(i), id)
			}
			return nil
		},
	}
}

func newDepCyclesCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "cycles",
		Short: "List dependency cycles; exits non-zero when any exist",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			cycles, err := app.core.DependencyCycles(app.ctx)
			if err != nil {
				return err
			}
			if app.json {
				if err := render.EmitMany(app.out, cycles); err != nil {
					return err
				}
			} else {
				for _, c := range cycles {
					fmt.Fprintf(app.out, "cycle: %v\n", idStrings(c))
				}
			}
			if len(cycles) > 0 {
				return errors.New("dependency cycles found")
			}
			return nil
		},
	}
}

func idStrings(ids []model.ID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func indent(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += "  "
	}
	return out
}
