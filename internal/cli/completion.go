package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tedkulp/drops/internal/core"
	"github.com/tedkulp/drops/internal/model"
)

func newCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "completion <bash|zsh|fish>",
		Short:       "Generate a shell completion script",
		Args:        exactArgs(1),
		Annotations: noStore(),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), true)
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return invalidArgs("unsupported shell %q; choose bash, zsh, or fish", args[0])
			}
		},
	}
}

func completeIssueIDs(app *App, maxArgs int) cobra.CompletionFunc {
	return completeIssueIDsWith(app, maxArgs, func(filter *core.IssueFilter) error {
		return applyStatuses(filter, nil, app.includeClosed)
	})
}

// completeClosedIssueIDs is reopen's complement: a reopen operates on a closed
// issue, so a live-only default would list nothing it can act on.
func completeClosedIssueIDs(app *App, maxArgs int) cobra.CompletionFunc {
	return completeIssueIDsWith(app, maxArgs, func(filter *core.IssueFilter) error {
		filter.Statuses = []model.Status{model.StatusClosed}
		return nil
	})
}

func completeIssueIDsWith(app *App, maxArgs int, configure func(*core.IssueFilter) error) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if maxArgs >= 0 && len(args) >= maxArgs {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		filter, ok, err := scopedIssueFilter(app, 0)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		if !ok {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		if err := configure(&filter); err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		issues, err := app.core.Issues(app.ctx, filter)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		used := make(map[string]bool, len(args))
		for _, arg := range args {
			used[arg] = true
		}
		completions := make([]cobra.Completion, 0, len(issues))
		for _, issue := range issues {
			id := string(issue.ID)
			if !used[id] && strings.HasPrefix(id, toComplete) {
				completions = append(completions, cobra.CompletionWithDesc(id, completionDescription(issue.Title)))
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

func completionDescription(value string) string {
	return strings.NewReplacer("\t", " ", "\r", " ", "\n", " ").Replace(value)
}

func registerFixedFlagCompletion(cmd *cobra.Command, flag string, choices ...string) {
	completions := make([]cobra.Completion, len(choices))
	copy(completions, choices)
	if err := cmd.RegisterFlagCompletionFunc(flag,
		cobra.FixedCompletions(completions, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
}

func registerIssueTypeCompletion(cmd *cobra.Command) {
	registerFixedFlagCompletion(cmd, "type",
		string(model.TypeTask),
		string(model.TypeBug),
		string(model.TypeFeature),
		string(model.TypeEpic),
		string(model.TypeChore),
		string(model.TypeResearch),
		string(model.TypeDecision),
	)
}

func registerStatusCompletion(cmd *cobra.Command, all bool) {
	choices := []string{
		string(model.StatusOpen),
		string(model.StatusInProgress),
		string(model.StatusClosed),
	}
	if all {
		choices = append(choices, "all")
	}
	registerFixedFlagCompletion(cmd, "status", choices...)
}

func registerPriorityCompletion(cmd *cobra.Command) {
	registerFixedFlagCompletion(cmd, "priority", "0", "1", "2", "3", "4")
}

func completeProjectSlugs(app *App, includeArchived bool) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		projects, err := app.core.Projects(app.ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		completions := make([]cobra.Completion, 0, len(projects))
		for _, project := range projects {
			if (includeArchived || project.ArchivedAt == nil) && strings.HasPrefix(project.Slug, toComplete) {
				completions = append(completions, project.Slug)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeLabels(app *App) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		counts, err := app.labelCounts()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		completions := make([]cobra.Completion, 0, len(counts))
		for label := range counts {
			if strings.HasPrefix(label, toComplete) {
				completions = append(completions, label)
			}
		}
		sort.Strings(completions)
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

func registerDynamicFlagCompletion(cmd *cobra.Command, flag string, completion cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(flag, completion); err != nil {
		panic(err)
	}
}
