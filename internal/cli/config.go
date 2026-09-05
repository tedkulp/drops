package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/gitx"
	"github.com/tedkulp/drops/internal/render"
	"github.com/tedkulp/drops/internal/resolve"
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
			scoped, found, routed, resolveErr := app.scopedProjectRouting(false)
			if found {
				resolvedSlug = scoped.Slug
			}
			facts := routed.Request.Git
			if app.inbox {
				// --inbox names the project without consulting the
				// ladder, so there is no resolution to report. The
				// directory is still a directory, and reporting no
				// Git root would say "not in a repository" of a
				// checkout.
				if _, inspected, err := app.gitFacts(); err == nil {
					facts = inspected
				}
			}
			settings := configSettings(*dbPath, resolvedSlug, facts, routed.Result.Rule)
			if err := emitConfig(app, settings); err != nil {
				return err
			}
			// The refusal is the exit code, but the evidence goes out
			// first. An origin two projects claim is precisely the
			// question this verb exists to answer, and it was the one
			// question it could not: the resolver's error came back
			// before a single fact printed.
			return resolveErr
		},
	})
	return cfg
}

// configSettings is the effective configuration and the evidence behind it.
//
// The evidence is the half of a routing question the store cannot answer: the
// rung that answered, the Git root rung 3 compared against its bindings, and
// the NORMALIZED origin rung 4 matched against its locators. The normalized
// form is the one a reader cannot reproduce by hand — `git remote get-url
// origin` prints the raw URL and the ladder never compares that — which is why
// it is here rather than left to the caller.
//
// Every key but store.path goes absent rather than empty when there is nothing
// to say, so "no origin" and "an origin that normalizes to nothing" are not
// reported as an empty string that reads like a match.
func configSettings(dbPath, resolvedSlug string, facts gitx.Facts, rule resolve.Rule) map[string]string {
	out := map[string]string{
		"store.path": dbPath,
	}
	if resolvedSlug != "" {
		out["project.slug"] = resolvedSlug
	}
	if name := rule.String(); name != "" {
		out["project.rule"] = name
	}
	if facts.InRepository {
		out["git.root"] = facts.Root
	}
	if origin := resolve.NormalizeLocator(facts.RemoteURL); origin != "" {
		out["git.origin"] = origin
	}
	return out
}

// emitConfig writes the settings in whichever of the two forms was asked for.
//
// config show is the verb an agent reaches for when a command answered the way
// it did and nobody knows why, so it owes --json a real object rather than the
// key=value lines a parser would have to split.
func emitConfig(app *App, settings map[string]string) error {
	if app.json {
		return render.EmitOne(app.out, settings)
	}
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := fmt.Fprintf(app.out, "%s=%s\n", k, settings[k]); err != nil {
			return err
		}
	}
	return nil
}
