package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/render"
	"github.com/tedkulp/drops/internal/sync"
)

func registerSyncCmds(root *cobra.Command, app *App) {
	root.AddCommand(newSyncCmd(app))
	root.AddCommand(newReplicaCmd(app))
}

func (a *App) transport() *sync.Transport {
	return sync.New(a.core, a.store, sync.Options{})
}

func newSyncCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Pull, import, export, and push the mirror in one cycle",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := app.transport().Sync(app.ctx)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, res)
			}
			if res.Imported {
				fmt.Fprintf(app.out, "imported %d, ignored %d\n", res.Applied, res.Ignored)
			}
			if res.Exported {
				fmt.Fprintf(app.out, "exported at %s\n", res.Head)
			}
			if !res.Imported && !res.Exported {
				fmt.Fprintln(app.out, "nothing to sync")
			}
			for _, c := range res.Conflicts {
				fmt.Fprintf(app.out, "conflict: %v\n", c)
			}
			return nil
		},
	}
}

func newReplicaCmd(app *App) *cobra.Command {
	replica := &cobra.Command{Use: "replica", Short: "Manage the replica key"}
	replica.AddCommand(&cobra.Command{
		Use:   "rekey",
		Short: "Rotate the active replica key",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := app.transport().Rekey(app.ctx)
			if err != nil {
				return err
			}
			if app.json {
				return render.EmitOne(app.out, map[string]string{"replica_key": string(key)})
			}
			fmt.Fprintln(app.out, key)
			return nil
		},
	})
	return replica
}
