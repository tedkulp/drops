package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newConfigCmd(app *App, dbPath *string) *cobra.Command {
	cfg := &cobra.Command{
		Use:         "config",
		Short:       "Inspect drops configuration",
		Args:        unknownSubcommand(),
		RunE:        func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		Annotations: noStore(),
	}
	cfg.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print effective configuration as sorted key=value lines",
		Args:  noArgs(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolvedSlug := ""
			scoped, found, err := app.scopedProject(false)
			if err != nil {
				return err
			}
			if found {
				resolvedSlug = scoped.Slug
			}
			settings := configSettings(*dbPath, resolvedSlug)
			keys := make([]string, 0, len(settings))
			for k := range settings {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(app.out, "%s=%s\n", k, settings[k])
			}
			return nil
		},
	})
	return cfg
}

func configSettings(dbPath, resolvedSlug string) map[string]string {
	out := map[string]string{
		"store.path": dbPath,
	}
	if resolvedSlug != "" {
		out["project.slug"] = resolvedSlug
	}
	return out
}
