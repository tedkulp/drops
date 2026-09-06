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

// positionalCompletion and flagCompletion are one cobra function type in two
// roles that disagree about what `args` means, which is what let 99c27 hide in
// plain sight. For a ValidArgsFunction, `args` is the positionals cobra has
// already accepted, and consulting them is the whole point. For a flag's
// completion function cobra passes the command's positionals too, where they
// say nothing about the value being typed.
//
// The flag role is a struct rather than a second defined func type because
// cobra.CompletionFunc is an *alias* for the bare func type. Two func types
// would leave both roles assignable to a plain cobra.CompletionFunc and so to
// each other, and the next helper written the natural way would reintroduce
// the bug silently. Wrapping makes the roles mutually unassignable: nothing
// reaches registerDynamicFlagCompletion without being built as a flag
// completion, and a flag completion cannot be set as a ValidArgsFunction.
type positionalCompletion cobra.CompletionFunc

type flagCompletion struct {
	complete cobra.CompletionFunc
}

func completeIssueIDs(app *App, maxArgs int) positionalCompletion {
	return completeIssueIDsWith(app, maxArgs, liveStatusFilter(app))
}

// liveStatusFilter is the id-completion default, shared by both roles: open
// and in_progress, list's contract, widened only by -a.
func liveStatusFilter(app *App) func(*core.IssueFilter) error {
	return func(filter *core.IssueFilter) error {
		return applyStatuses(filter, nil, app.includeClosed)
	}
}

// completeClosedIssueIDs is reopen's complement: a reopen operates on a closed
// issue, so a live-only default would list nothing it can act on.
func completeClosedIssueIDs(app *App, maxArgs int) positionalCompletion {
	return completeIssueIDsWith(app, maxArgs, func(filter *core.IssueFilter) error {
		filter.Statuses = []model.Status{model.StatusClosed}
		return nil
	})
}

// completeIssueIDsWith is the positional role, and both uses of `args` are
// deliberate: the arity guard stops offering ids past the last id-shaped
// position, so `comment add <id> <body>` completes nothing for the body, and
// the ids already accepted are not offered a second time.
func completeIssueIDsWith(app *App, maxArgs int, configure func(*core.IssueFilter) error) positionalCompletion {
	return func(_ *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if maxArgs >= 0 && len(args) >= maxArgs {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		used := make(map[string]bool, len(args))
		for _, arg := range args {
			used[arg] = true
		}
		return issueIDCompletions(app, configure, used, toComplete)
	}
}

// completeIssueIDsForFlag is the flag role: it consults neither the count nor
// the contents of the positionals, so `create "a title" --parent <TAB>` offers
// the same ids as `create --parent <TAB>`. That is the order anyone types.
func completeIssueIDsForFlag(app *App) flagCompletion {
	return flagCompletion{complete: func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return issueIDCompletions(app, liveStatusFilter(app), nil, toComplete)
	}}
}

func issueIDCompletions(app *App, configure func(*core.IssueFilter) error, used map[string]bool,
	toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
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
	completions := make([]cobra.Completion, 0, len(issues))
	for _, issue := range issues {
		id := string(issue.ID)
		if !used[id] && strings.HasPrefix(id, toComplete) {
			completions = append(completions, cobra.CompletionWithDesc(id, completionDescription(issue.Title)))
		}
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
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

func completeProjectSlugs(app *App, includeArchived bool) flagCompletion {
	return flagCompletion{complete: func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
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
	}}
}

func completeLabels(app *App) flagCompletion {
	return flagCompletion{complete: func(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
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
	}}
}

func registerDynamicFlagCompletion(cmd *cobra.Command, flag string, completion flagCompletion) {
	if err := cmd.RegisterFlagCompletionFunc(flag, completion.complete); err != nil {
		panic(err)
	}
}
